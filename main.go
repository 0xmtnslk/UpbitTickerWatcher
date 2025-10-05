package main

import (
        "encoding/json"
        "fmt"
        "io"
        "log"
        "net/http"
        "os"
        "regexp"
        "sync"
        "time"
)

type Announcement struct {
        ID        int    `json:"id"`
        Title     string `json:"title"`
        CreatedAt string `json:"created_at"`
}

type APIResponse struct {
        Data struct {
                Notices []Announcement `json:"notices"`
        } `json:"data"`
}

type Listing struct {
        Symbol     string `json:"symbol"`
        Timestamp  string `json:"timestamp"`
        DetectedAt string `json:"detected_at"`
}

type ListingsData struct {
        Listings []Listing `json:"listings"`
}

var (
        mu              sync.Mutex
        seenSymbols     = make(map[string]bool)
        tickerRegex     = regexp.MustCompile(`\(([A-Z0-9]+)\)`)
        koreanPattern   = regexp.MustCompile(`신규\s*거래지원\s*안내`)
        englishPattern  = regexp.MustCompile(`Market Support for`)
)

func main() {
        log.Println("Upbit API Monitor başlatılıyor...")
        
        if err := loadExistingListings(); err != nil {
                log.Printf("Mevcut kayıtlar yüklenirken hata: %v\n", err)
        }

        ticker := time.NewTicker(2 * time.Second)
        defer ticker.Stop()

        log.Println("API izleme başladı (2 saniye aralıklarla)")
        
        for range ticker.C {
                checkAnnouncements()
        }
}

func loadExistingListings() error {
        mu.Lock()
        defer mu.Unlock()

        data, err := os.ReadFile("upbit_new.json")
        if err != nil {
                if os.IsNotExist(err) {
                        return nil
                }
                return err
        }

        var listingsData ListingsData
        if err := json.Unmarshal(data, &listingsData); err != nil {
                return err
        }

        for _, listing := range listingsData.Listings {
                seenSymbols[listing.Symbol] = true
        }

        log.Printf("Mevcut %d coin yüklendi: %v\n", len(seenSymbols), getSymbolList())
        return nil
}

func getSymbolList() []string {
        symbols := make([]string, 0, len(seenSymbols))
        for symbol := range seenSymbols {
                symbols = append(symbols, symbol)
        }
        return symbols
}

func checkAnnouncements() {
        client := &http.Client{Timeout: 10 * time.Second}
        req, err := http.NewRequest("GET", "https://api-manager.upbit.com/api/v1/announcements?os=web&page=1&per_page=20&category=all", nil)
        if err != nil {
                log.Printf("İstek oluşturma hatası: %v\n", err)
                return
        }

        req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
        req.Header.Set("Referer", "https://upbit.com")
        req.Header.Set("Accept", "application/json")

        resp, err := client.Do(req)
        if err != nil {
                log.Printf("API isteği hatası: %v\n", err)
                return
        }
        defer resp.Body.Close()

        remainingReq := resp.Header.Get("Remaining-Req")
        if remainingReq != "" {
                log.Printf("Rate Limit: %s\n", remainingReq)
        }

        if resp.StatusCode != http.StatusOK {
                log.Printf("API yanıt hatası: %d\n", resp.StatusCode)
                if resp.StatusCode == 429 {
                        retryAfter := resp.Header.Get("Retry-After")
                        if retryAfter != "" {
                                log.Printf("Retry-After: %s saniye\n", retryAfter)
                        }
                }
                return
        }

        body, err := io.ReadAll(resp.Body)
        if err != nil {
                log.Printf("Yanıt okuma hatası: %v\n", err)
                return
        }

        var apiResp APIResponse
        if err := json.Unmarshal(body, &apiResp); err != nil {
                log.Printf("JSON parse hatası: %v\n", err)
                return
        }

        for _, announcement := range apiResp.Data.Notices {
                if isNewListingAnnouncement(announcement.Title) {
                        if symbol := extractSymbol(announcement.Title); symbol != "" {
                                if err := saveNewListing(symbol, announcement.CreatedAt); err != nil {
                                        log.Printf("Kayıt hatası: %v\n", err)
                                }
                        }
                }
        }
}

func isNewListingAnnouncement(title string) bool {
        return koreanPattern.MatchString(title) || englishPattern.MatchString(title)
}

func extractSymbol(title string) string {
        matches := tickerRegex.FindStringSubmatch(title)
        if len(matches) > 1 {
                return matches[1]
        }
        return ""
}

func saveNewListing(symbol, createdAt string) error {
        mu.Lock()
        defer mu.Unlock()

        if seenSymbols[symbol] {
                return nil
        }

        now := time.Now().UTC()
        
        timestamp := now.Format("2006-01-02T15:04:05.000Z")
        detectedAt := now.Format("2006-01-02 15:04:05.000 UTC")

        newListing := Listing{
                Symbol:     symbol,
                Timestamp:  timestamp,
                DetectedAt: detectedAt,
        }

        data, err := os.ReadFile("upbit_new.json")
        if err != nil && !os.IsNotExist(err) {
                return err
        }

        var listingsData ListingsData
        if len(data) > 0 {
                if err := json.Unmarshal(data, &listingsData); err != nil {
                        return err
                }
        }

        listingsData.Listings = append([]Listing{newListing}, listingsData.Listings...)

        jsonData, err := json.MarshalIndent(listingsData, "", "  ")
        if err != nil {
                return err
        }

        if err := os.WriteFile("upbit_new.json", jsonData, 0644); err != nil {
                return err
        }

        seenSymbols[symbol] = true
        
        log.Printf("🚀 YENİ COIN TESPİT EDİLDİ: %s - %s\n", symbol, detectedAt)
        fmt.Printf("\n==============================================\n")
        fmt.Printf("✨ YENİ LİSTİNG TESPİT EDİLDİ!\n")
        fmt.Printf("Symbol: %s\n", symbol)
        fmt.Printf("Tespit Zamanı: %s\n", detectedAt)
        fmt.Printf("==============================================\n\n")

        return nil
}
