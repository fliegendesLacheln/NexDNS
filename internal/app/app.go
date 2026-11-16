package app

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"nexdns/internal/appdns"
	"nexdns/internal/diagnostics"
	"nexdns/internal/discord"
	"nexdns/internal/dns"
	"nexdns/internal/network"
	"nexdns/internal/settings"
	"nexdns/internal/traffic"
)

type DashboardState struct {
	InternetConnected bool                    `json:"internetConnected"`
	ActiveProviderID  string                  `json:"activeProviderId"`
	ActiveProvider    string                  `json:"activeProvider"`
	ActiveAdapterName string                  `json:"activeAdapterName"`
	ActiveAdapterType string                  `json:"activeAdapterType"`
	ActiveIPv4        string                  `json:"activeIpv4"`
	ActiveMode        string                  `json:"activeMode"` // "system" or "app"
	DNSLatencyMs      int64                   `json:"dnsLatencyMs"`
	DNSStatus         string                  `json:"dnsStatus"`
	Traffic           traffic.TrafficSnapshot `json:"traffic"`
	ActiveRulesCount  int                     `json:"activeRulesCount"`
	HasBackup         bool                    `json:"hasBackup"`
	ProxyRunning      bool                    `json:"proxyRunning"`
}

type Controller struct {
	mu          sync.RWMutex
	trafficMon  *traffic.Monitor
	proxy       *appdns.LocalDNSProxy
	cachedState DashboardState
	lastCheck   time.Time

	adapterMu   sync.RWMutex
	adapterInfo *network.AdapterInfo

	dnsMu        sync.RWMutex
	dnsLatencyMs int64
	dnsStatus    string

	rulesMu          sync.RWMutex
	activeRulesCount int

	refreshOnce sync.Once
	refreshStop chan struct{}
}

var globalController *Controller

func GetController() *Controller {
	if globalController == nil {
		globalController = &Controller{
			trafficMon:   traffic.GetMonitor(),
			proxy:        appdns.GetProxy(),
			dnsLatencyMs: -1,
			dnsStatus:    "checking",
		}
	}
	return globalController
}

func (c *Controller) Init() error {
	cfg := settings.Load()

	// Detect primary network adapter and set monitor target.
	adapter, err := network.GetPrimaryActiveAdapter()
	if err == nil && adapter != nil {
		c.setAdapter(adapter)
		c.trafficMon.SetInterfaceIndex(uint32(adapter.InterfaceID))
	}
	c.trafficMon.Start()

	// If mode is App Mode, start local DNS proxy.
	if cfg.Mode == "app" {
		_ = c.proxy.Start("127.0.0.1:53")
	}

	c.refreshRulesCount()
	c.startBackgroundRefresh()
	return nil
}

func (c *Controller) GetDashboardState() DashboardState {
	cfg := settings.Get()

	adapter := c.getAdapter()
	var adapterName, adapterType, ipv4 string
	if adapter != nil {
		adapterName = adapter.Name
		adapterType = adapter.Type
		ipv4 = adapter.IPv4
	} else {
		adapterName = "Algılanamadı"
		adapterType = "Ethernet"
		ipv4 = "-"
	}

	// Active DNS Provider information
	providerName := "Cloudflare (1.1.1.1)"
	for _, p := range dns.GetDefaultProviders() {
		if p.ID == cfg.ActiveProvider {
			providerName = fmt.Sprintf("%s (%s)", p.Name, p.IPv4Primary)
			break
		}
	}

	c.dnsMu.RLock()
	lat := c.dnsLatencyMs
	dnsStatus := c.dnsStatus
	c.dnsMu.RUnlock()

	c.rulesMu.RLock()
	activeRulesCount := c.activeRulesCount
	c.rulesMu.RUnlock()

	state := DashboardState{
		InternetConnected: lat >= 0 || (adapter != nil && adapter.Gateway != ""),
		ActiveProviderID:  cfg.ActiveProvider,
		ActiveProvider:    providerName,
		ActiveAdapterName: adapterName,
		ActiveAdapterType: adapterType,
		ActiveIPv4:        ipv4,
		ActiveMode:        cfg.Mode,
		DNSLatencyMs:      lat,
		DNSStatus:         dnsStatus,
		Traffic:           c.trafficMon.GetCurrentStats(),
		ActiveRulesCount:  activeRulesCount,
		HasBackup:         dns.HasBackup(),
		ProxyRunning:      c.proxy.IsRunning(),
	}

	c.mu.Lock()
	c.cachedState = state
	c.mu.Unlock()

	return state
}

func (c *Controller) setAdapter(adapter *network.AdapterInfo) {
	c.adapterMu.Lock()
	c.adapterInfo = adapter
	c.adapterMu.Unlock()
}

func (c *Controller) getAdapter() *network.AdapterInfo {
	c.adapterMu.RLock()
	defer c.adapterMu.RUnlock()
	return c.adapterInfo
}

// refreshAdapter re-detects the primary adapter (expensive PowerShell call) but
// only resets the traffic monitor baseline when the interface actually changes.
func (c *Controller) refreshAdapter() {
	adapter, err := network.GetPrimaryActiveAdapter()
	if err != nil || adapter == nil {
		return
	}
	prev := c.getAdapter()
	if prev == nil || prev.InterfaceID != adapter.InterfaceID {
		c.trafficMon.SetInterfaceIndex(uint32(adapter.InterfaceID))
	}
	c.setAdapter(adapter)
}

func (c *Controller) refreshLatency() {
	cfg := settings.Get()
	targetDNS := []string{}
	if cfg.Mode == "app" {
		if c.proxy.IsRunning() {
			targetDNS = append(targetDNS, "127.0.0.1")
		}
	} else if adapter := c.getAdapter(); adapter != nil && len(adapter.DNSServers) > 0 {
		targetDNS = adapter.DNSServers
	}
	var lat int64 = -1
	var err error = fmt.Errorf("active DNS server is unavailable")
	for _, server := range targetDNS {
		lat, err = dns.MeasureDNSLatency(server, 1500*time.Millisecond)
		if err == nil && lat >= 0 {
			break
		}
	}

	c.dnsMu.Lock()
	defer c.dnsMu.Unlock()
	if err != nil || lat < 0 {
		c.dnsLatencyMs = -1
		c.dnsStatus = "offline"
	} else {
		c.dnsLatencyMs = lat
		c.dnsStatus = "online"
	}
}

func (c *Controller) refreshRulesCount() {
	rules := appdns.LoadRules()
	count := 0
	for _, r := range rules {
		if r.Enabled {
			count++
		}
	}
	c.rulesMu.Lock()
	c.activeRulesCount = count
	c.rulesMu.Unlock()
}

// startBackgroundRefresh performs the expensive state detection (PowerShell
// adapter lookup, DNS latency probe, rules reload) in the background so the
// 1-second GetDashboardState polling loop never blocks the UI.
func (c *Controller) startBackgroundRefresh() {
	c.refreshOnce.Do(func() {
		c.refreshStop = make(chan struct{})
		go func() {
			c.refreshAdapter()
			c.refreshLatency()

			adapterTicker := time.NewTicker(10 * time.Second)
			latencyTicker := time.NewTicker(5 * time.Second)
			rulesTicker := time.NewTicker(15 * time.Second)
			defer adapterTicker.Stop()
			defer latencyTicker.Stop()
			defer rulesTicker.Stop()

			for {
				select {
				case <-c.refreshStop:
					return
				case <-adapterTicker.C:
					c.refreshAdapter()
				case <-latencyTicker.C:
					c.refreshLatency()
				case <-rulesTicker.C:
					c.refreshRulesCount()
				}
			}
		}()
	})
}

func (c *Controller) SwitchMode(mode string) error {
	if mode != "system" && mode != "app" {
		return fmt.Errorf("geçersiz çalışma modu: %s", mode)
	}

	cfg := settings.Get()
	cfg.Mode = mode
	_ = settings.Save(cfg)

	adapter, _ := network.GetPrimaryActiveAdapter()
	if adapterName := func() string {
		if adapter != nil {
			return adapter.Name
		}
		return ""
	}(); adapterName != "" {
		if mode == "app" {
			// In App Mode, we start local DNS proxy and point system DNS to 127.0.0.1
			_ = c.proxy.Start("127.0.0.1:53")
			_ = dns.ApplySystemDNS(adapterName, "127.0.0.1", "", "", "")
		} else {
			// In System Mode, stop proxy and restore/apply provider DNS
			c.proxy.Stop()
			for _, p := range dns.GetDefaultProviders() {
				if p.ID == cfg.ActiveProvider {
					_ = dns.ApplySystemDNS(adapterName, p.IPv4Primary, p.IPv4Secondary, p.IPv6Primary, p.IPv6Secondary)
					break
				}
			}
		}
	}

	return nil
}

func (c *Controller) GetProviders() []dns.Provider {
	providers := dns.GetDefaultProviders()
	cfg := settings.Get()
	for _, c := range cfg.CustomDNS {
		providers = append(providers, dns.Provider{
			ID:            "custom_" + c.Name,
			Name:          c.Name,
			Tagline:       "Özel Kullanıcı DNS Sunucusu",
			IPv4Primary:   c.IPv4Primary,
			IPv4Secondary: c.IPv4Secondary,
			IPv6Primary:   c.IPv6Primary,
			IPv6Secondary: c.IPv6Secondary,
			Features:      []string{"Özel Tanımlı"},
			LatencyMs:     -1,
			Status:        "untested",
		})
	}
	return providers
}

func (c *Controller) MeasureProviders() []dns.Provider {
	providers := c.GetProviders()
	return dns.MeasureAllProviders(providers)
}

func (c *Controller) MeasureProvider(providerID string) dns.Provider {
	for _, p := range c.GetProviders() {
		if p.ID == providerID {
			return dns.MeasureSingleProvider(p)
		}
	}
	return dns.Provider{ID: providerID, LatencyMs: -1, Status: "offline"}
}

func (c *Controller) ApplyProvider(providerID string) error {
	cfg := settings.Get()
	providers := c.GetProviders()
	var selected *dns.Provider
	for _, p := range providers {
		if p.ID == providerID {
			selected = &p
			break
		}
	}
	if selected == nil {
		return fmt.Errorf("sağlayıcı bulunamadı: %s", providerID)
	}

	cfg.ActiveProvider = providerID
	_ = settings.Save(cfg)

	adapter, err := network.GetPrimaryActiveAdapter()
	if err != nil || adapter == nil {
		return fmt.Errorf("aktif ağ adaptörü tespit edilemedi")
	}

	if cfg.Mode == "system" {
		return dns.ApplySystemDNS(adapter.Name, selected.IPv4Primary, selected.IPv4Secondary, selected.IPv6Primary, selected.IPv6Secondary)
	} else {
		c.proxy.SetDefaultDNS(selected.IPv4Primary)
		return nil
	}
}

func (c *Controller) ApplyCustomDNS(name, primary, secondary, primary6, secondary6 string) error {
	adapter, err := network.GetPrimaryActiveAdapter()
	if err != nil || adapter == nil {
		return fmt.Errorf("aktif ağ adaptörü bulunamadı")
	}

	cfg := settings.Get()
	cfg.CustomDNS = append(cfg.CustomDNS, settings.CustomDNSConfig{
		Name:          name,
		IPv4Primary:   primary,
		IPv4Secondary: secondary,
		IPv6Primary:   primary6,
		IPv6Secondary: secondary6,
	})
	cfg.ActiveProvider = "custom_" + name
	_ = settings.Save(cfg)

	return dns.ApplySystemDNS(adapter.Name, primary, secondary, primary6, secondary6)
}

func (c *Controller) ResetToDHCP() error {
	adapter, err := network.GetPrimaryActiveAdapter()
	if err != nil || adapter == nil {
		return fmt.Errorf("aktif ağ adaptörü bulunamadı")
	}
	return dns.ResetToDHCP(adapter.Name)
}

func (c *Controller) RestoreBackup() error {
	return dns.RestoreFromLatestBackup()
}

func (c *Controller) FlushDNS() error {
	return dns.FlushDNSCache()
}

func (c *Controller) GetAdapters() ([]network.AdapterInfo, error) {
	return network.GetNetworkAdapters()
}

func (c *Controller) GetTrafficStats() traffic.TrafficSnapshot {
	return c.trafficMon.GetCurrentStats()
}

func (c *Controller) GetTrafficHistory() []traffic.TrafficSnapshot {
	return c.trafficMon.GetHistory()
}

func (c *Controller) RunDiscordDiagnostics() discord.DiscordDiagnosticResult {
	return discord.RunDiscordChecks()
}

func (c *Controller) RunNetworkDiagnostics() diagnostics.DiagnosticsSummary {
	return diagnostics.RunAllDiagnostics()
}

func (c *Controller) GetRunningApps() ([]appdns.RunningProcess, error) {
	return appdns.GetRunningApplications()
}

func (c *Controller) GetAppRules() []appdns.AppRule {
	return appdns.LoadRules()
}

func (c *Controller) AddAppRule(ruleJSON string) error {
	var rule appdns.AppRule
	if err := json.Unmarshal([]byte(ruleJSON), &rule); err != nil {
		return err
	}
	if rule.ID == "" {
		rule.ID = fmt.Sprintf("rule_%d", time.Now().UnixNano())
	}
	return appdns.AddOrUpdateRule(rule)
}

func (c *Controller) DeleteAppRule(ruleID string) error {
	return appdns.DeleteRule(ruleID)
}

func (c *Controller) ToggleAppRule(ruleID string, enabled bool) error {
	return appdns.ToggleRule(ruleID, enabled)
}

func (c *Controller) GetSettings() *settings.Config {
	return settings.Get()
}

func (c *Controller) SaveSettings(cfgJSON string) error {
	var cfg settings.Config
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		return err
	}
	return settings.Save(&cfg)
}

func (c *Controller) OpenFilePicker() (string, error) {
	// Native Windows PowerShell File Open Dialog
	psScript := `Add-Type -AssemblyName System.Windows.Forms;
	$f = New-Object System.Windows.Forms.OpenFileDialog;
	$f.Filter = "Executable Files (*.exe)|*.exe|All Files (*.*)|*.*";
	$f.Title = "NEXDNS - Uygulama EXE Dosyası Seç";
	if ($f.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
		$f.FileName
	}`
	cmd := exec.Command("powershell", "-NoProfile", "-STA", "-Command", psScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
