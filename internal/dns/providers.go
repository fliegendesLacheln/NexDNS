package dns

type Provider struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Tagline       string   `json:"tagline"`
	IPv4Primary   string   `json:"ipv4Primary"`
	IPv4Secondary string   `json:"ipv4Secondary"`
	IPv6Primary   string   `json:"ipv6Primary"`
	IPv6Secondary string   `json:"ipv6Secondary"`
	Features      []string `json:"features"`
	LatencyMs     int64    `json:"latencyMs"` // -1 means untested or unreachable
	Status        string   `json:"status"`    // "online", "slow", "offline", "untested"
}

func GetDefaultProviders() []Provider {
	return []Provider{
		{
			ID:            "cloudflare",
			Name:          "Cloudflare",
			Tagline:       "Ultra Hızlı & Gizlilik Odaklı (1.1.1.1)",
			IPv4Primary:   "1.1.1.1",
			IPv4Secondary: "1.0.0.1",
			IPv6Primary:   "2606:4700:4700::1111",
			IPv6Secondary: "2606:4700:4700::1001",
			Features:      []string{"Yüksek Hız", "DNSSEC", "Sıfır Log"},
			LatencyMs:     -1,
			Status:        "untested",
		},
		{
			ID:            "google",
			Name:          "Google Public DNS",
			Tagline:       "Küresel Güvenilirlik & Geniş Kapsama (8.8.8.8)",
			IPv4Primary:   "8.8.8.8",
			IPv4Secondary: "8.8.4.4",
			IPv6Primary:   "2001:4860:4860::8888",
			IPv6Secondary: "2001:4860:4860::8844",
			Features:      []string{"Küresel Anycast", "DDoS Koruması", "DNSSEC"},
			LatencyMs:     -1,
			Status:        "untested",
		},
		{
			ID:            "quad9",
			Name:          "Quad9",
			Tagline:       "Zararlı Yazılım & Oltalama Korumalı (9.9.9.9)",
			IPv4Primary:   "9.9.9.9",
			IPv4Secondary: "149.112.112.112",
			IPv6Primary:   "2620:fe::fe",
			IPv6Secondary: "2620:fe::9",
			Features:      []string{"Zararlı Yazılım Engelleme", "Gizlilik", "DNSSEC"},
			LatencyMs:     -1,
			Status:        "untested",
		},
		{
			ID:            "adguard",
			Name:          "AdGuard DNS",
			Tagline:       "Reklam & Takipçi Engelleyici (94.140.14.14)",
			IPv4Primary:   "94.140.14.14",
			IPv4Secondary: "94.140.15.15",
			IPv6Primary:   "2a10:50c0::ad1:ff",
			IPv6Secondary: "2a10:50c0::ad2:ff",
			Features:      []string{"Reklam Engelleme", "Takipçi Filtreleme", "Gizlilik"},
			LatencyMs:     -1,
			Status:        "untested",
		},
		{
			ID:            "opendns",
			Name:          "OpenDNS (Cisco)",
			Tagline:       "Cisco Güvencesi & Güvenli Gezinme (208.67.222.222)",
			IPv4Primary:   "208.67.222.222",
			IPv4Secondary: "208.67.220.220",
			IPv6Primary:   "2620:119:35::35",
			IPv6Secondary: "2620:119:53::53",
			Features:      []string{"Cisco Altyapısı", "Phishing Koruması", "Yüksek Uptime"},
			LatencyMs:     -1,
			Status:        "untested",
		},
	}
}
