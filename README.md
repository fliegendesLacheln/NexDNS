# NEXDNS — Advanced Network & DNS Manager for Windows

**NEXDNS**, Windows 10 ve Windows 11 için geliştirilmiş; modern, tamamen ücretsiz, yüksek performanslı ve taşınabilir bir DNS ve ağ yönetimi masaüstü uygulamasıdır.

Bu proje bir web sitesi veya tarayıcı paneli değildir. Saf Go ve yerel Microsoft Edge WebView2 çekirdeği ile çalışan, C derleyicisi (MinGW/GCC) gerektirmeyen, tek parça bağımsız bir Windows masaüstü yazılımıdır.

---

## Öne Çıkan Özellikler

### 1. İki Farklı DNS Çalışma Modu
* **Tüm Sistem DNS Modu:**  
  * Aktif Ethernet ve Wi-Fi ağ adaptörünü otomatik algılar.
  * IPv4 ve IPv6 birincil/ikincil DNS adreslerini doğrudan Windows ağ katmanına uygular.
  * Değişiklikten önce mevcut DNS ayarlarını otomatik yedekler (JSON).
  * Tek tıkla önceki DNS ayarlarına geri dönme veya otomatik DHCP'ye geçme imkânı sunar.
  * Her değişiklik sonrası Windows DNS çözümleme önbelleğini (`DnsFlushResolverCache` ve `ipconfig /flushdns`) temizler.
* **Uygulama Bazlı DNS Modu (deneysel):**  
  * Çalışan Windows masaüstü uygulamalarını algılar ve listeler.
  * Manuel olarak `.exe` dosyası seçebilme imkânı sunar.
  * Uygulama başına DNS sağlayıcısı kuralı oluşturmayı destekler (örn. Discord → Quad9, Chrome → Cloudflare).
  * Yerel DNS Proxy (`127.0.0.1:53`) ve Windows Socket IP Helper API (`GetExtendedUdpTable`) ile süreç eşleştirmesi için kod içerir. Uygulama bazlı yönlendirme tüm uygulamalarda doğrulanmamıştır; Windows izinleri ve uygulama içi DoH davranışı sonucu etkileyebilir.

### 2. Ücretsiz & Güvenli DNS Sağlayıcıları
* **Cloudflare:** `1.1.1.1` & `1.0.0.1` (IPv6: `2606:4700:4700::1111`)
* **Google Public DNS:** `8.8.8.8` & `8.8.4.4` (IPv6: `2001:4860:4860::8888`)
* **Quad9:** `9.9.9.9` & `149.112.112.112` (IPv6: `2620:fe::fe`)
* **AdGuard DNS:** `94.140.14.14` & `94.140.15.15` (IPv6: `2a10:50c0::ad1:ff`)
* **OpenDNS (Cisco):** `208.67.222.222` & `208.67.220.220` (IPv6: `2620:119:35::35`)
* **Özel DNS Ekleme:** Kullanıcının dilediği IPv4 ve IPv6 adreslerini ekleyebilmesi.
* **Gerçek Ping/Gecikme Ölçümü:** UDP port 53'e doğrudan RFC 1035 A-query paketi göndererek RTT ölçümü yapılır (asla sahte veri üretilmez).

### 3. VIP Tema Sistemi (7 Özel Tema)
Ayarlar menüsünden anında geçiş yapılabilen 7 profesyonel görsel tema:
1. **Midnight Purple:** Koyu zemin, mor ve menekşe neon vurgular.
2. **Cyber Neon:** Koyu zemin, neon mavi ve turkuaz siber parlama.
3. **Obsidian Black:** Saf minimalist siyah, füme ve antrasit.
4. **Arctic Blue:** Buz mavisi ve lacivert teknik görünüm.
5. **Emerald Pro:** Koyu zümrüt yeşili ve modern fintech stili.
6. **Crimson Elite:** Derin antrasit ve kan kırmızı performans stili.
7. **Light Professional:** Açık tema, yumuşak gri ve mavi kurumsal görünüm.

### 4. Gerçek Zamanlı Trafik & Ağ İzleyici
* Windows IP Helper API (`GetIfEntry2`) ile aktif ağ kartından doğrudan okunan anlık indirme (Download Bps) ve yükleme (Upload Bps) hızları.
* Oturum boyunca indirilen ve yüklenen toplam veri miktarı.
* Harici JS kütüphanesi gerektirmeyen, donanım hızlandırmalı saf Canvas canlı trafik grafiği.
* Adaptör adı, MAC adresi, yerel IPv4, IPv6, varsayılan ağ geçidi ve bağlantı hızı.

### 5. Özel Discord Bağlantı Testi
Discord'a erişim sorunlarını derinlemesine incelemek için 5 aşamalı test:
1. **DNS Çözümleme:** `discord.com` A ve AAAA kayıtlarının çözümlenme durumu.
2. **Web Ana Servis:** `https://discord.com` HTTPS bağlantı durumu.
3. **REST API / Gateway:** `https://discord.com/api/v9/gateway` rota kontrolü.
4. **CDN & Medya:** `cdn.discordapp.com` görsel ve medya sunucu erişimi.
5. **Canlı Ses & Sohbet Soketi:** `gateway.discord.gg:443` TCP el sıkışması ve RTT süresi.

### 6. Sistem Ağ Tanılaması (Diagnostics)
* 8 aşamalı derin ağ sağlık analizi.
* 0 - 100 arası genel sağlık skoru.
* Olası sorunlar için kullanıcıya özel çözüm önerileri.

---

## Teknik Mimari

```
NEXDNS/
├── cmd/nexdns/
│   └── main.go                 # Giriş noktası, Win32 pencere yaşam döngüsü, IPC
├── internal/
│   ├── app/                    # Ana uygulama kontrolcüsü, metrik toplayıcı
│   ├── dns/                    # Netsh, DnsFlush, ping ölçümü, backup & rollback
│   ├── appdns/                 # Süreç listesi, UDP port PID eşleştirici, DNS Proxy
│   ├── network/                # Adaptör bilgileri (IP, MAC, Gateway, Speed)
│   ├── traffic/                # Windows MIB_IF_ROW2 ile gerçek hız ölçümü
│   ├── discord/                # Discord servis tanılamaları
│   ├── diagnostics/            # 8 adımlı ağ ve internet sağlık testleri
│   └── settings/               # %APPDATA%\NEXDNS altında saklanan yerel ayarlar
├── ui/
│   ├── dist/
│   │   ├── index.html          # SPA masaüstü arayüzü
│   │   ├── css/                # main.css ve 7 VIP tema tanımları
│   │   └── js/                 # app.js ve charts.js
│   └── ui.go                   # embed.FS ile frontend'in binary içine gömülmesi
├── build/
│   ├── build.bat               # Tek tıkla Windowsgui EXE derleme
│   └── run.bat                 # Başlatma betiği
├── NEXDNS.exe                  # Tek parça taşınabilir Windows uygulaması
├── go.mod
└── README.md
```

---

## Kurulum ve Çalıştırma

### Hazır Binary ile Çalıştırma:
Klasördeki `NEXDNS.exe` dosyasını doğrudan çalıştırabilirsiniz.

> **Önemli Not:** Windows ağ adaptörünün DNS adreslerini değiştirebilmek için Windows işletim sistemi yönetici (Administrator) izni gerektirir. DNS değiştirme özelliğinin tam çalışabilmesi için uygulamayı **"Yönetici Olarak Çalıştır"** seçeneğiyle açmanız önerilir.

### Kaynak Koddan Derleme:
```powershell
go build -ldflags "-H windowsgui -s -w" -o NEXDNS.exe ./cmd/nexdns
```
veya `build/build.bat` dosyasını çalıştırın.

---

## Bilinen Teknik Sınırlar (Dürüstlük İlkesi)
* **DoH (DNS-over-HTTPS) Kullanan Uygulamalar:**  
  Chrome, Firefox veya Brave gibi tarayıcılarda dahili "Güvenli DNS (DoH)" özelliği aktifse, uygulama DNS sorgularını yerel 53 portuna göndermeyip şifreli HTTPS üzerinden çıkarabilir. DoH'u kapatmak gerekli olabilir, ancak bu tek başına uygulama bazlı DNS yönlendirmesinin çalıştığını doğrulamaz.
* **Ücretli Servis Bağımlılığı:**  
  NEXDNS hiçbir ücretli API veya lisans anahtarına bağımlı değildir; %100 açık ve ücretsiz standart internet altyapısını kullanır.
