package main

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net"
    "net/http"
    "net/url"
    "os"
    "regexp"
    "strings"
    "sync"
    "sync/atomic"
    "time"

    "github.com/gorilla/websocket"
    "golang.org/x/net/proxy"
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

// WebSocket mesaj tipleri
type TickerData struct {
    Type   string  `json:"ty"`
    Code   string  `json:"cd"`
    Price  float64 `json:"tp"`
    Change string  `json:"c"`
}

var (
    mu             sync.Mutex
    seenSymbols    = make(map[string]bool)
    seenMarkets    = make(map[string]bool)
    tickerRegex    = regexp.MustCompile(`\(([A-Z0-9]+)\)`)
    koreanPattern  = regexp.MustCompile(`신규\s*거래지원\s*안내`)
    englishPattern = regexp.MustCompile(`Market Support for`)
    proxyList      []string
    proxyCounter   uint32
)

func main() {
    log.Println("🚀 Upbit Hybrid Monitor başlatılıyor...")
    
    loadProxies()
    
    if err := loadExistingListings(); err != nil {
        log.Printf("Mevcut kayıtlar yüklenirken hata: %v\n", err)
    }

    // Mevcut marketleri yükle
    loadExistingMarkets()

    // Hybrid monitoring: WebSocket + REST API
    go startWebSocketMonitoring()
    go startRESTAPIMonitoring()

    // Ana thread'i canlı tut
    select {}
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
    } else {
        log.Printf("✅ %d proxy yüklendi\n", len(proxyList))
    }
}

func getNextProxy() string {
    if len(proxyList) == 0 {
        return ""
    }
    
    idx := atomic.AddUint32(&proxyCounter, 1) - 1
    return proxyList[idx%uint32(len(proxyList))]
}

func createWebSocketDialer(proxyURL string) *websocket.Dialer {
    dialer := &websocket.Dialer{
        HandshakeTimeout: 10 * time.Second,
    }

    if proxyURL != "" {
        parsedProxy, err := url.Parse(proxyURL)
        if err == nil {
            if strings.HasPrefix(parsedProxy.Scheme, "socks5") {
                auth := &proxy.Auth{}
                if parsedProxy.User != nil {
                    auth.User = parsedProxy.User.Username()
                    auth.Password, _ = parsedProxy.User.Password()
                }

                proxyDialer, err := proxy.SOCKS5("tcp", parsedProxy.Host, auth, proxy.Direct)
                if err == nil {
                    dialer.NetDial = func(network, addr string) (net.Conn, error) {
                        return proxyDialer.Dial(network, addr)
                    }
                }
            } else {
                dialer.Proxy = http.ProxyURL(parsedProxy)
            }
        }
    }

    return dialer
}

func startWebSocketMonitoring() {
    log.Println("📡 WebSocket monitoring başlatılıyor...")
    
    for {
        proxyURL := getNextProxy()
        dialer := createWebSocketDialer(proxyURL)
        
        headers := http.Header{}
        headers.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
        headers.Set("Origin", "https://upbit.com")
        
        conn, _, err := dialer.Dial("wss://crix-ws-first.upbit.com/websocket", headers)
        if err != nil {
            log.Printf("WebSocket bağlantı hatası: %v, 5 saniye sonra tekrar denenecek\n", err)
            time.Sleep(5 * time.Second)
            continue
        }

        log.Println("✅ WebSocket bağlantısı kuruldu")

        // Tüm KRW marketlerini takip et
        payload := []map[string]interface{}{
            {
                "ticket": "upbit-monitor",
            },
            {
                "type":   "ticker",
                "codes":  []string{"KRW-BTC"}, // Başlangıç için sadece BTC
                "isOnlySnapshot": false,
            },
        }

        if err := conn.WriteJSON(payload); err != nil {
            log.Printf("WebSocket payload gönderme hatası: %v\n", err)
            conn.Close()
            time.Sleep(5 * time.Second)
            continue
        }

        // Mesajları dinle
        for {
            _, message, err := conn.ReadMessage()
            if err != nil {
                log.Printf("WebSocket mesaj okuma hatası: %v\n", err)
                break
            }

            // Yeni market kontrolü
            go processWebSocketMessage(message)
        }

        conn.Close()
        log.Println("WebSocket bağlantısı kesildi, yeniden bağlanılıyor...")
        time.Sleep(2 * time.Second)
    }
}

func processWebSocketMessage(message []byte) {
    var ticker TickerData
    if err := json.Unmarshal(message, &ticker); err != nil {
        return
    }

    if ticker.Code != "" {
        mu.Lock()
        if !seenMarkets[ticker.Code] {
            seenMarkets[ticker.Code] = true
            mu.Unlock()
            
            // Yeni market tespit edildi!
            if strings.HasPrefix(ticker.Code, "KRW-") {
                symbol := strings.TrimPrefix(ticker.Code, "KRW-")
                log.Printf("🆕 WebSocket'ten yeni market tespit edildi: %s\n", ticker.Code)
                saveNewListingFromWebSocket(symbol)
            }
        } else {
            mu.Unlock()
        }
    }
}

func startRESTAPIMonitoring() {
    log.Println("🔄 REST API monitoring başlatılıyor...")
    
    ticker := time.NewTicker(30 * time.Second) // Daha az sıklıkta
    defer ticker.Stop()

    for range ticker.C {
        checkAnnouncements()
        updateWebSocketMarkets() // Yeni marketleri WebSocket'e ekle
    }
}

func updateWebSocketMarkets() {
    // Mevcut tüm KRW marketlerini al ve WebSocket'i güncelle
    markets := getAllKRWMarkets()
    if len(markets) > 0 {
        log.Printf("📊 %d market WebSocket'te takip ediliyor\n", len(markets))
    }
}

func getAllKRWMarkets() []string {
    proxyURL := getNextProxy()
    client := createHTTPClient(proxyURL)
    
    req, err := http.NewRequest("GET", "https://api.upbit.com/v1/market/all", nil)
    if err != nil {
        return nil
    }

    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
    
    resp, err := client.Do(req)
    if err != nil {
        return nil
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        return nil
    }

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return nil
    }

    var markets []map[string]interface{}
    if err := json.Unmarshal(body, &markets); err != nil {
        return nil
    }

    var krwMarkets []string
    for _, market := range markets {
        if marketCode, ok := market["market"].(string); ok {
            if strings.HasPrefix(marketCode, "KRW-") {
                krwMarkets = append(krwMarkets, marketCode)
                
                mu.Lock()
                seenMarkets[marketCode] = true
                mu.Unlock()
            }
        }
    }

    return krwMarkets
}

func loadExistingMarkets() {
    markets := getAllKRWMarkets()
    log.Printf("📈 %d mevcut KRW market yüklendi\n", len(markets))
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
            if strings.HasPrefix(parsedProxy.Scheme, "socks5") {
                auth := &proxy.Auth{}
                if parsedProxy.User != nil {
                    auth.User = parsedProxy.User.Username()
                    auth.Password, _ = parsedProxy.User.Password()
                }

                dialer, err := proxy.SOCKS5("tcp", parsedProxy.Host, auth, proxy.Direct)
                if err == nil {
                    transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
                        return dialer.Dial(network, addr)
                    }
                }
            } else {
                transport.Proxy = http.ProxyURL(parsedProxy)
            }
        }
    }

    return &http.Client{
        Timeout:   15 * time.Second,
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

    log.Printf("📋 Mevcut %d coin yüklendi\n", len(seenSymbols))
    return nil
}

func checkAnnouncements() {
    proxyURL := getNextProxy()
    client := createHTTPClient(proxyURL)
    
    req, err := http.NewRequest("GET", "https://api-manager.upbit.com/api/v1/announcements?os=web&page=1&per_page=20&category=all", nil)
    if err != nil {
        return
    }

    req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
    req.Header.Set("Referer", "https://upbit.com")
    req.Header.Set("Accept", "application/json")

    resp, err := client.Do(req)
    if err != nil {
        log.Printf("API isteği hatası: %v\n", err)
        return
    }
    defer resp.Body.Close()

    if resp.StatusCode == 429 {
        retryAfter := resp.Header.Get("Retry-After")
        log.Printf("⏳ Rate limit (429), %s saniye beklenecek\n", retryAfter)
        return
    }

    if resp.StatusCode != http.StatusOK {
        return
    }

    body, err := io.ReadAll(resp.Body)
    if err != nil {
        return
    }

    var apiResp APIResponse
    if err := json.Unmarshal(body, &apiResp); err != nil {
        return
    }

    for _, announcement := range apiResp.Data.Notices {
        if isNewListingAnnouncement(announcement.Title) {
            if symbol := extractSymbol(announcement.Title); symbol != "" {
                saveNewListing(symbol, announcement.CreatedAt)
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
    return saveNewListingInternal(symbol, createdAt, "REST API")
}

func saveNewListingFromWebSocket(symbol string) error {
    return saveNewListingInternal(symbol, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), "WebSocket")
}

func saveNewListingInternal(symbol, createdAt, source string) error {
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
    
    log.Printf("🚀 YENİ COIN TESPİT EDİLDİ (%s): %s - %s\n", source, symbol, detectedAt)
    fmt.Printf("\n🎉 ================================\n")
    fmt.Printf("✨ YENİ LİSTİNG TESPİT EDİLDİ!\n")
    fmt.Printf("📊 Kaynak: %s\n", source)
    fmt.Printf("🪙 Symbol: %s\n", symbol)
    fmt.Printf("⏰ Tespit Zamanı: %s\n", detectedAt)
    fmt.Printf("🎉 ================================\n\n")

    return nil
}
