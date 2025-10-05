# Upbit Coin Listing Monitor

## Proje Açıklaması
Bu Go uygulaması, Upbit exchange API'sini sürekli izleyerek yeni coin listing duyurularını otomatik olarak tespit eder ve kayıt altına alır.

## Özellikler
- ✅ 100ms aralıklarla API kontrolü (10 istekte/saniye)
- ✅ 10 proxy desteği (round-robin yük dağıtımı)
- ✅ Korece ve İngilizce duyuru tespiti
- ✅ Regex ile otomatik coin ticker çıkarma
- ✅ Milisaniye hassasiyetinde timestamp kaydı
- ✅ Tekrar tespit önleme sistemi
- ✅ JSON formatında veri saklama

## Nasıl Çalışır
1. Uygulama her 100ms'de bir Upbit API'sine istek atar
2. 10 proxy sırayla kullanılır (her proxy ~1 saniyede 1 istek alır)
3. Duyuru başlıklarında "신규 거래지원 안내" veya "Market Support for" araması yapar
4. Parantez içindeki coin ticker'ını (örn: MIRA, BTC) tespit eder
5. İlk kez tespit edilen coin'leri `upbit_new.json` dosyasına kaydeder

## Proxy Ayarları
Proxy kullanmak için 10 proxy URL'ini environment variable olarak ayarlayın:
- `PROXY_1`, `PROXY_2`, ..., `PROXY_10`
- Format: `http://host:port` veya `http://username:password@host:port`

Örnek workflow komutu:
```bash
PROXY_1=http://proxy1.com:8080 PROXY_2=http://proxy2.com:8080 go run main.go
```

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
- Standart library (net/http, encoding/json, regexp)
- Round-robin proxy rotation
- Atomic counter ile thread-safe proxy seçimi
