package discord

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type CheckItemResult struct {
	Name        string `json:"name"`
	Target      string `json:"target"`
	Status      string `json:"status"` // "success", "warning", "error"
	StatusText  string `json:"statusText"`
	LatencyMs   int64  `json:"latencyMs"`
	Description string `json:"description"`
	Details     string `json:"details"`
}

type DiscordDiagnosticResult struct {
	OverallStatus string            `json:"overallStatus"` // "success", "partial", "error"
	TestedAt      time.Time         `json:"testedAt"`
	Items         []CheckItemResult `json:"items"`
}

// RunDiscordChecks performs real tests against Discord services.
func RunDiscordChecks() DiscordDiagnosticResult {
	results := make([]CheckItemResult, 5)
	var wg sync.WaitGroup
	wg.Add(5)

	// 1. DNS Resolution Test
	go func() {
		defer wg.Done()
		start := time.Now()
		ips, err := net.LookupIP("discord.com")
		lat := time.Since(start).Milliseconds()
		if err != nil {
			results[0] = CheckItemResult{
				Name:        "DNS Çözümleme Testi",
				Target:      "discord.com (DNS Query)",
				Status:      "error",
				StatusText:  "DNS Çözümleme Başarısız",
				LatencyMs:   -1,
				Description: "Discord alan adı IP adresine çevrilemedi.",
				Details:     err.Error(),
			}
		} else {
			var ipStrs []string
			for _, ip := range ips {
				ipStrs = append(ipStrs, ip.String())
			}
			results[0] = CheckItemResult{
				Name:        "DNS Çözümleme Testi",
				Target:      "discord.com (A/AAAA)",
				Status:      "success",
				StatusText:  "Başarılı",
				LatencyMs:   lat,
				Description: fmt.Sprintf("%d adet IP adresi başarıyla çözümlendi.", len(ips)),
				Details:     strings.Join(ipStrs, ", "),
			}
		}
	}()

	// 2. Discord Web Ana Servis Erişimi
	go func() {
		defer wg.Done()
		client := &http.Client{
			Timeout: 4 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
			},
		}
		start := time.Now()
		resp, err := client.Get("https://discord.com")
		lat := time.Since(start).Milliseconds()
		if err != nil {
			results[1] = CheckItemResult{
				Name:        "Discord Ana Servisi",
				Target:      "https://discord.com",
				Status:      "error",
				StatusText:  "Sunucuya Erişilemiyor",
				LatencyMs:   -1,
				Description: "Discord web servisine HTTPS bağlantısı kurulamadı.",
				Details:     err.Error(),
			}
		} else {
			_ = resp.Body.Close()
			results[1] = CheckItemResult{
				Name:        "Discord Ana Servisi",
				Target:      "https://discord.com",
				Status:      "success",
				StatusText:  "Erişilebilir",
				LatencyMs:   lat,
				Description: fmt.Sprintf("HTTP Durum: %d OK", resp.StatusCode),
				Details:     fmt.Sprintf("Protokol: %s", resp.Proto),
			}
		}
	}()

	// 3. Discord API Bağlantı Testi
	go func() {
		defer wg.Done()
		client := &http.Client{
			Timeout: 4 * time.Second,
		}
		start := time.Now()
		req, _ := http.NewRequest("GET", "https://discord.com/api/v9/gateway", nil)
		req.Header.Set("User-Agent", "NEXDNS-Diagnostic-Client/1.0")
		resp, err := client.Do(req)
		lat := time.Since(start).Milliseconds()
		if err != nil {
			results[2] = CheckItemResult{
				Name:        "Discord API / Gateway",
				Target:      "https://discord.com/api/v9/gateway",
				Status:      "error",
				StatusText:  "Zaman Aşımı / Hata",
				LatencyMs:   -1,
				Description: "Discord REST API ve Gateway endpoint'ine ulaşılamadı.",
				Details:     err.Error(),
			}
		} else {
			_ = resp.Body.Close()
			results[2] = CheckItemResult{
				Name:        "Discord API / Gateway",
				Target:      "https://discord.com/api/v9/gateway",
				Status:      "success",
				StatusText:  "API Yanıt Veriyor",
				LatencyMs:   lat,
				Description: fmt.Sprintf("API Durum Kodu: %d", resp.StatusCode),
				Details:     "Gateway rota testi başarılı.",
			}
		}
	}()

	// 4. Discord CDN (Görsel ve Medya Sunucuları)
	go func() {
		defer wg.Done()
		client := &http.Client{
			Timeout: 4 * time.Second,
		}
		start := time.Now()
		resp, err := client.Head("https://cdn.discordapp.com/attachments")
		lat := time.Since(start).Milliseconds()
		if err != nil {
			// Try get fallback
			startFallback := time.Now()
			resp2, err2 := client.Get("https://cdn.discordapp.com")
			latFallback := time.Since(startFallback).Milliseconds()
			if err2 != nil {
				results[3] = CheckItemResult{
					Name:        "Discord CDN & Medya",
					Target:      "https://cdn.discordapp.com",
					Status:      "error",
					StatusText:  "CDN Erişimi Yok",
					LatencyMs:   -1,
					Description: "Medya ve avatar dağıtım ağına bağlanılamadı.",
					Details:     err.Error(),
				}
				return
			}
			_ = resp2.Body.Close()
			results[3] = CheckItemResult{
				Name:        "Discord CDN & Medya",
				Target:      "https://cdn.discordapp.com",
				Status:      "success",
				StatusText:  "CDN Aktif",
				LatencyMs:   latFallback,
				Description: "Medya sunucuları erişilebilir durumda.",
				Details:     "CDN yanıt verdi.",
			}
			return
		}
		_ = resp.Body.Close()
		results[3] = CheckItemResult{
			Name:        "Discord CDN & Medya",
			Target:      "https://cdn.discordapp.com",
			Status:      "success",
			StatusText:  "CDN Aktif",
			LatencyMs:   lat,
			Description: "Görsel ve dosya dağıtım ağı sorunsuz çalışıyor.",
			Details:     fmt.Sprintf("HTTP %d", resp.StatusCode),
		}
	}()

	// 5. Discord Voice & Socket Gateway TCP Testi
	go func() {
		defer wg.Done()
		start := time.Now()
		conn, err := net.DialTimeout("tcp", "gateway.discord.gg:443", 3*time.Second)
		lat := time.Since(start).Milliseconds()
		if err != nil {
			results[4] = CheckItemResult{
				Name:        "Canlı Ses & Gateway Soketi",
				Target:      "gateway.discord.gg:443",
				Status:      "error",
				StatusText:  "TCP Portu Kapalı / Engelli",
				LatencyMs:   -1,
				Description: "Discord ses ve sohbet soket kapısına TCP el sıkışması başarısız oldu.",
				Details:     err.Error(),
			}
		} else {
			_ = conn.Close()
			results[4] = CheckItemResult{
				Name:        "Canlı Ses & Gateway Soketi",
				Target:      "gateway.discord.gg:443",
				Status:      "success",
				StatusText:  "Soket Açık & Bağlı",
				LatencyMs:   lat,
				Description: "Gateway TCP el sıkışması başarıyla tamamlandı.",
				Details:     fmt.Sprintf("RTT Gecikmesi: %d ms", lat),
			}
		}
	}()

	wg.Wait()

	overall := "success"
	hasError := false
	hasSuccess := false
	for _, it := range results {
		if it.Status == "error" {
			hasError = true
		} else if it.Status == "success" {
			hasSuccess = true
		}
	}
	if hasError && hasSuccess {
		overall = "partial"
	} else if hasError && !hasSuccess {
		overall = "error"
	}

	return DiscordDiagnosticResult{
		OverallStatus: overall,
		TestedAt:      time.Now(),
		Items:         results,
	}
}
