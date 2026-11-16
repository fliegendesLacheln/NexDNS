package diagnostics

import (
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"nexdns/internal/dns"
	"nexdns/internal/network"
)

type DiagnosticItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"` // "success", "warning", "error", "running"
	StatusText  string `json:"statusText"`
	LatencyMs   int64  `json:"latencyMs"`
	Description string `json:"description"`
	Solution    string `json:"solution"`
}

type DiagnosticsSummary struct {
	TestedAt time.Time        `json:"testedAt"`
	Score    int              `json:"score"` // 0 - 100
	Items    []DiagnosticItem `json:"items"`
}

func RunAllDiagnostics() DiagnosticsSummary {
	var items []DiagnosticItem
	var mu sync.Mutex
	var wg sync.WaitGroup

	addItem := func(item DiagnosticItem) {
		mu.Lock()
		defer mu.Unlock()
		items = append(items, item)
	}

	adapter, _ := network.GetPrimaryActiveAdapter()

	// 1. Adaptör Bağlantı Durumu
	wg.Add(1)
	go func() {
		defer wg.Done()
		if adapter != nil && adapter.IsActive {
			addItem(DiagnosticItem{
				ID:          "adapter",
				Title:       "Ağ Adaptörü Durumu",
				Status:      "success",
				StatusText:  "Bağlantı Aktif (Up)",
				LatencyMs:   0,
				Description: fmt.Sprintf("Aktif adaptör: %s (%s, Hız: %s)", adapter.Name, adapter.Type, adapter.LinkSpeed),
			})
		} else {
			addItem(DiagnosticItem{
				ID:          "adapter",
				Title:       "Ağ Adaptörü Durumu",
				Status:      "error",
				StatusText:  "Adaptör Bulunamadı / Kapalı",
				LatencyMs:   -1,
				Description: "Etkin bir ağ adaptörü tespit edilemedi.",
				Solution:    "Ağ kablonuzu veya Wi-Fi bağlantınızı kontrol edin.",
			})
		}
	}()

	// 2. Varsayılan Ağ Geçidi (Gateway)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if adapter != nil && adapter.Gateway != "" {
			start := time.Now()
			conn, err := net.DialTimeout("tcp", net.JoinHostPort(adapter.Gateway, "80"), 1500*time.Millisecond)
			lat := time.Since(start).Milliseconds()
			if err == nil {
				_ = conn.Close()
				addItem(DiagnosticItem{
					ID:          "gateway",
					Title:       "Varsayılan Ağ Geçidi (Router)",
					Status:      "success",
					StatusText:  "Ağ Geçidi Erişilebilir",
					LatencyMs:   lat,
					Description: fmt.Sprintf("Yerel yönlendirici IP: %s", adapter.Gateway),
				})
				return
			}
			// Ping fallback via ping command
			cmd := exec.Command("ping", "-n", "1", "-w", "1000", adapter.Gateway)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			if pingErr := cmd.Run(); pingErr == nil {
				addItem(DiagnosticItem{
					ID:          "gateway",
					Title:       "Varsayılan Ağ Geçidi (Router)",
					Status:      "success",
					StatusText:  "Ağ Geçidi Yanıt Veriyor (ICMP)",
					LatencyMs:   lat,
					Description: fmt.Sprintf("Ağ geçidi IP: %s", adapter.Gateway),
				})
			} else {
				addItem(DiagnosticItem{
					ID:          "gateway",
					Title:       "Varsayılan Ağ Geçidi (Router)",
					Status:      "warning",
					StatusText:  "Yanıt Alınamadı",
					LatencyMs:   -1,
					Description: fmt.Sprintf("Ağ geçidi (%s) ping isteklerine yanıt vermedi (Modem güvenlik duvarı ICMP'yi engelliyor olabilir).", adapter.Gateway),
					Solution:    "İnternet bağlantınız çalışıyorsa bu durum modem güvenlik politikasından kaynaklanabilir.",
				})
			}
		} else {
			addItem(DiagnosticItem{
				ID:          "gateway",
				Title:       "Varsayılan Ağ Geçidi (Router)",
				Status:      "error",
				StatusText:  "Ağ Geçidi Yok",
				LatencyMs:   -1,
				Description: "Adaptörde atanmış bir varsayılan ağ geçidi (Default Gateway) bulunmuyor.",
				Solution:    "DHCP ayarlarınızı kontrol edin veya modemi yeniden başlatın.",
			})
		}
	}()

	// 3. Genel İnternet Erişimi
	wg.Add(1)
	go func() {
		defer wg.Done()
		client := &http.Client{Timeout: 3 * time.Second}
		start := time.Now()
		resp, err := client.Get("http://www.msftconnecttest.com/connecttest.txt")
		lat := time.Since(start).Milliseconds()
		if err == nil && resp.StatusCode == 200 {
			_ = resp.Body.Close()
			addItem(DiagnosticItem{
				ID:          "internet",
				Title:       "Küresel İnternet Bağlantısı",
				Status:      "success",
				StatusText:  "İnternet Bağlantısı Aktif",
				LatencyMs:   lat,
				Description: "Windows bağlantı kontrol noktası doğrulandı.",
			})
		} else {
			// fallback check
			conn, err2 := net.DialTimeout("tcp", "1.1.1.1:53", 2*time.Second)
			lat2 := time.Since(start).Milliseconds()
			if err2 == nil {
				_ = conn.Close()
				addItem(DiagnosticItem{
					ID:          "internet",
					Title:       "Küresel İnternet Bağlantısı",
					Status:      "success",
					StatusText:  "İnternet Bağlantısı Aktif (TCP 53)",
					LatencyMs:   lat2,
					Description: "Harici IP rotası erişilebilir.",
				})
			} else {
				addItem(DiagnosticItem{
					ID:          "internet",
					Title:       "Küresel İnternet Bağlantısı",
					Status:      "error",
					StatusText:  "İnternet Erişimi Yok",
					LatencyMs:   -1,
					Description: "Dış dünyaya IP düzeyinde veri iletimi sağlanamıyor.",
					Solution:    "Modeminizin internet (WAN/DSL) ışığını ve internet aboneliğinizi kontrol edin.",
				})
			}
		}
	}()

	// 4. DNS Çözümleme Yeteneği
	wg.Add(1)
	go func() {
		defer wg.Done()
		start := time.Now()
		ips, err := net.LookupIP("google.com")
		lat := time.Since(start).Milliseconds()
		if err == nil && len(ips) > 0 {
			addItem(DiagnosticItem{
				ID:          "dns_lookup",
				Title:       "DNS Alan Adı Çözümleme",
				Status:      "success",
				StatusText:  "DNS Çözümlemesi Başarılı",
				LatencyMs:   lat,
				Description: fmt.Sprintf("google.com çözümlendi (%d IP adresi)", len(ips)),
			})
		} else {
			addItem(DiagnosticItem{
				ID:          "dns_lookup",
				Title:       "DNS Alan Adı Çözümleme",
				Status:      "error",
				StatusText:  "DNS Çözümleme Hatası",
				LatencyMs:   -1,
				Description: "Alan adları IP adreslerine dönüştürülemiyor.",
				Solution:    "NEXDNS DNS Yöneticisi panelinden Cloudflare (1.1.1.1) veya Google DNS seçip uygulayın.",
			})
		}
	}()

	// 5. DNS Yanıt Süresi (RTT)
	wg.Add(1)
	go func() {
		defer wg.Done()
		targetDNS := "1.1.1.1"
		if adapter != nil && len(adapter.DNSServers) > 0 {
			targetDNS = adapter.DNSServers[0]
		}
		lat, err := dns.MeasureDNSLatency(targetDNS, 2500*time.Millisecond)
		if err == nil && lat >= 0 {
			status := "success"
			if lat > 80 {
				status = "warning"
			}
			addItem(DiagnosticItem{
				ID:          "dns_latency",
				Title:       "DNS Sorgu Yanıt Süresi",
				Status:      status,
				StatusText:  fmt.Sprintf("%d ms Gecikme", lat),
				LatencyMs:   lat,
				Description: fmt.Sprintf("Hedef DNS (%s) sorgusuna dönülen süre.", targetDNS),
			})
		} else {
			addItem(DiagnosticItem{
				ID:          "dns_latency",
				Title:       "DNS Sorgu Yanıt Süresi",
				Status:      "warning",
				StatusText:  "Ölçülemedi",
				LatencyMs:   -1,
				Description: fmt.Sprintf("DNS sunucusundan (%s) doğrudan UDP yanıtı alınamadı.", targetDNS),
				Solution:    "Sağlayıcınızı daha düşük gecikmeli bir sunucuyla (örn. Cloudflare) değiştirin.",
			})
		}
	}()

	// 6. IPv4 Bağlantı Durumu
	wg.Add(1)
	go func() {
		defer wg.Done()
		if adapter != nil && adapter.IPv4 != "" {
			addItem(DiagnosticItem{
				ID:          "ipv4",
				Title:       "IPv4 Yapılandırması",
				Status:      "success",
				StatusText:  "IPv4 Aktif & Atanmış",
				LatencyMs:   0,
				Description: fmt.Sprintf("Yerel IPv4: %s", adapter.IPv4),
			})
		} else {
			addItem(DiagnosticItem{
				ID:          "ipv4",
				Title:       "IPv4 Yapılandırması",
				Status:      "error",
				StatusText:  "IPv4 Adresi Yok",
				LatencyMs:   -1,
				Description: "Adaptörde geçerli bir yerel IPv4 adresi tespit edilemedi.",
				Solution:    "Ağ adaptörünüzün DHCP istemcisini veya statik IP ayarlarını kontrol edin.",
			})
		}
	}()

	// 7. IPv6 Desteği
	wg.Add(1)
	go func() {
		defer wg.Done()
		if adapter != nil && adapter.IPv6 != "" {
			addItem(DiagnosticItem{
				ID:          "ipv6",
				Title:       "IPv6 Desteği",
				Status:      "success",
				StatusText:  "IPv6 Etkin",
				LatencyMs:   0,
				Description: fmt.Sprintf("Global/Yerel IPv6: %s", adapter.IPv6),
			})
		} else {
			addItem(DiagnosticItem{
				ID:          "ipv6",
				Title:       "IPv6 Desteği",
				Status:      "warning",
				StatusText:  "IPv6 Bulunmuyor",
				LatencyMs:   0,
				Description: "İnternet servis sağlayıcınız veya modeminiz IPv6 tahsis etmemiş.",
				Solution:    "İsteğe bağlı bir protokoldür; internet kullanımınızı engellemez.",
			})
		}
	}()

	// 8. DNS Yapılandırma & Güvenlik Kontrolü
	wg.Add(1)
	go func() {
		defer wg.Done()
		if adapter != nil && len(adapter.DNSServers) > 0 {
			dnsList := strings.Join(adapter.DNSServers, ", ")
			addItem(DiagnosticItem{
				ID:          "dns_config",
				Title:       "DNS Sunucu Yapılandırması",
				Status:      "success",
				StatusText:  "DNS Sunucuları Atanmış",
				LatencyMs:   0,
				Description: fmt.Sprintf("Kayıtlı sunucular: %s", dnsList),
			})
		} else {
			addItem(DiagnosticItem{
				ID:          "dns_config",
				Title:       "DNS Sunucu Yapılandırması",
				Status:      "warning",
				StatusText:  "DNS Sunucusu Belirtilmemiş",
				LatencyMs:   -1,
				Description: "Adaptörde tanımlı DNS sunucusu tespit edilemedi.",
				Solution:    "DNS Manager ekranından güvenilir bir DNS seçip uygulayın.",
			})
		}
	}()

	wg.Wait()

	// Calculate score
	score := 100
	for _, it := range items {
		if it.Status == "error" {
			score -= 25
		} else if it.Status == "warning" {
			score -= 10
		}
	}
	if score < 0 {
		score = 0
	}

	return DiagnosticsSummary{
		TestedAt: time.Now(),
		Score:    score,
		Items:    items,
	}
}
