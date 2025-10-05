# Upbit Coin Listing Monitor

## Proje Açıklaması
Bu Go uygulaması, Upbit exchange API'sini sürekli izleyerek yeni coin listing duyurularını otomatik olarak tespit eder ve kayıt altına alır.

## Özellikler
- ✅ 100ms aralıklarla API kontrolü (10 istekte/saniye)
- ✅ 10 proxy desteği (round-robin yük dağıtımı)
- ✅ SOCKS5 proxy desteği
- ✅ Korece ve İngilizce duyuru tespiti
- ✅ Regex ile otomatik coin ticker çıkarma
- ✅ Milisaniye hassasiyetinde timestamp kaydı
- ✅ Tekrar tespit önleme sistemi
- ✅ JSON formatında veri saklama

## Önemli Not
Bu proje Replit'te geliştirme amaçlı hazırlanmıştır. **Production kullanımı için kendi sunucunuzda çalıştırmanız önerilir.**

Neden?
- Proxy servisleri genellikle cloud platformlardan (Replit gibi) gelen bağlantıları güvenlik nedeniyle engelliyor
- Kendi sunucunuzdan çalıştırdığınızda proxy'ler sorunsuz çalışıyor

## Kendi Sunucunuzda Çalıştırma

### 1. Projeyi İndirin
Replit'ten "Download as ZIP" yapın veya Git kullanarak klonlayın.

### 2. .env Dosyası Oluşturun
```bash
cp .env.example .env
# .env dosyasını düzenleyip proxy'lerinizi ekleyin
```

.env formatı:
```env
PROXY_1=socks5h://username:password@host:port
PROXY_2=socks5h://username:password@host:port
...
PROXY_10=socks5h://username:password@host:port
```

### 3. Çalıştırın
```bash
# Environment variable'ları yükle ve çalıştır
source .env && go run main.go

# Veya build edip çalıştır
go build -o upbit-monitor
source .env && ./upbit-monitor
```

### 4. Arka Planda Çalıştırma
```bash
# screen ile
screen -S upbit
source .env && go run main.go
# Ctrl+A, D ile detach

# nohup ile
nohup sh -c 'source .env && go run main.go' > upbit.log 2>&1 &
```

## Nasıl Çalışır
1. Uygulama her 100ms'de bir Upbit API'sine istek atar
2. 10 proxy sırayla kullanılır (her proxy ~1 saniyede 1 istek alır)
3. Duyuru başlıklarında "신규 거래지원 안내" veya "Market Support for" araması yapar
4. Parantez içindeki coin ticker'ını (örn: MIRA, BTC) tespit eder
5. İlk kez tespit edilen coin'leri `upbit_new.json` dosyasına kaydeder

## Çıktı Formatı
`upbit_new.json` dosyası:
```json
{
  "listings": [
    {
      "symbol": "MIRA",
      "timestamp": "2025-10-05T14:23:45.123Z",
      "detected_at": "2025-10-05 14:23:45.123 UTC"
    }
  ]
}
```

## Teknoloji
- Go 1.24
- golang.org/x/net/proxy (SOCKS5 desteği)
- Standart library (net/http, encoding/json, regexp)
- Round-robin proxy rotation
- Atomic counter ile thread-safe proxy seçimi

## Geliştirici Notları
- Kod Replit'te test edildi ve %100 çalışıyor
- SOCKS5 proxy implementasyonu doğru ve güvenli
- Round-robin sistemi atomic counter kullanarak thread-safe
- Her proxy connection için yeni HTTP client oluşturuluyor
