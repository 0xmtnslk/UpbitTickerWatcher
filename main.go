package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sync"
	"sync/atomic"
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
	proxyList       []string
	proxyCounter    uint32
)

func main() {
	log.Println("Upbit API Monitor başlatılıyor...")
	
	loadProxies()
	
	if err := loadExistingListings(); err != nil {
		log.Printf("Mevcut kayıtlar yüklenirken hata: %v\n", err)
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	log.Println("API izleme başladı (100ms aralıklarla)")
	
	for range ticker.C {
		checkAnnouncements()
	}
}

func loadProxies() {
	for i := 1; i <= 10; i++ {
		proxyURL := os.Getenv(fmt.Sprintf("PROXY_%d", i))
		if proxyURL != "" {
			proxyList = append(proxyList, proxyURL)
		}
	}

	if len(proxyList) == 0 {
		log.Println("⚠️  Hiç proxy ayarlanmamış. Direkt bağlantı kullanılacak.")
		log.Println("   Proxy kullanmak için PROXY_1, PROXY_2, ..., PROXY_10 environment variable'larını ayarlayın.")
		log.Println("   Örnek: PROXY_1=http://proxy1.example.com:8080")
	} else {
		log.Printf("✅ %d proxy yüklendi:\n", len(proxyList))
		for i, proxy := range proxyList {
			parsedURL, err := url.Parse(proxy)
			if err == nil {
				log.Printf("   Proxy %d: %s\n", i+1, parsedURL.Host)
			} else {
				log.Printf("   Proxy %d: %s (parse hatası: %v)\n", i+1, proxy, err)
			}
		}
	}
}

func getNextProxy() string {
	if len(proxyList) == 0 {
		return ""
	}
	
	idx := atomic.AddUint32(&proxyCounter, 1) - 1
	return proxyList[idx%uint32(len(proxyList))]
}

func createHTTPClient(proxyURL string) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: false,
	}

	if proxyURL != "" {
		parsedProxy, err := url.Parse(proxyURL)
		if err == nil {
			transport.Proxy = http.ProxyURL(parsedProxy)
		}
	}

	return &http.Client{
		Timeout:   10 * time.Second,
		Transport: transport,
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
	proxyURL := getNextProxy()
	client := createHTTPClient(proxyURL)
	
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
