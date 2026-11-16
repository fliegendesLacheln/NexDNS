package dns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
)

var (
	modDnsapi                 = syscall.NewLazyDLL("dnsapi.dll")
	procDnsFlushResolverCache = modDnsapi.NewProc("DnsFlushResolverCache")
)

// FlushDNSCache clears the Windows local DNS resolver cache.
func FlushDNSCache() error {
	// Call native Win32 DnsFlushResolverCache
	if procDnsFlushResolverCache.Find() == nil {
		r, _, _ := procDnsFlushResolverCache.Call()
		if r != 0 {
			// Succeeded
			return nil
		}
	}

	// Fallback to ipconfig /flushdns
	cmd := exec.Command("ipconfig", "/flushdns")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

// GetAdapterCurrentDNS fetches DNS addresses for a specific adapter name.
// It prefers a structured, locale-independent PowerShell query and falls back
// to best-effort netsh output parsing on localized Windows builds.
func GetAdapterCurrentDNS(adapterName string) (isDHCP bool, ipv4List []string, ipv6List []string, err error) {
	if ipv4, ipv6, dhcp, e := queryDNSServersPowerShell(adapterName); e == nil {
		return dhcp, ipv4, ipv6, nil
	}
	return queryDNSServersNetsh(adapterName)
}

func queryDNSServersPowerShell(adapterName string) (ipv4 []string, ipv6 []string, isDHCP bool, err error) {
	escaped := strings.ReplaceAll(adapterName, "'", "''")
	psScript := fmt.Sprintf(`$ErrorActionPreference = 'SilentlyContinue'
$adapter = Get-NetAdapter -Name '%s'
if (-not $adapter) { exit 1 }
$idx = $adapter.InterfaceIndex
$dns = Get-DnsClientServerAddress -InterfaceIndex $idx
$ipv4 = @($dns | Where-Object { $_.AddressFamily -eq 2 } | ForEach-Object { $_.ServerAddresses })
$ipv6 = @($dns | Where-Object { $_.AddressFamily -eq 23 } | ForEach-Object { $_.ServerAddresses })
$guid = $adapter.InterfaceGuid
$regPath = "HKLM:\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters\Interfaces\$guid"
$nameServer = (Get-ItemProperty -Path $regPath -Name NameServer).NameServer
$isDhcp = [string]::IsNullOrWhiteSpace($nameServer)
[PSCustomObject]@{ IsDhcp = $isDhcp; IPv4 = $ipv4; IPv6 = $ipv6 } | ConvertTo-Json -Compress
`, escaped)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil, nil, false, err
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return nil, nil, false, fmt.Errorf("adaptör DNS bilgisi alınamadı")
	}

	var parsed struct {
		IsDHCP bool     `json:"IsDhcp"`
		IPv4   []string `json:"IPv4"`
		IPv6   []string `json:"IPv6"`
	}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return nil, nil, false, err
	}
	return parsed.IPv4, parsed.IPv6, parsed.IsDHCP, nil
}

func queryDNSServersNetsh(adapterName string) (isDHCP bool, ipv4 []string, ipv6 []string, err error) {
	cmd := exec.Command("netsh", "interface", "ipv4", "show", "dnsservers", fmt.Sprintf("name=%s", adapterName))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, e := cmd.CombinedOutput(); e == nil {
		text := string(out)
		if strings.Contains(strings.ToLower(text), "dhcp") {
			isDHCP = true
		}
		ipv4 = extractIPv4Addresses(text)
	}

	cmd6 := exec.Command("netsh", "interface", "ipv6", "show", "dnsservers", fmt.Sprintf("name=%s", adapterName))
	cmd6.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out6, e := cmd6.CombinedOutput(); e == nil {
		ipv6 = extractIPv6Addresses(string(out6))
	}

	return isDHCP, ipv4, ipv6, nil
}

var ipv4AddressRe = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

func extractIPv4Addresses(text string) []string {
	var result []string
	seen := make(map[string]bool)
	for _, m := range ipv4AddressRe.FindAllString(text, -1) {
		if !seen[m] {
			seen[m] = true
			result = append(result, m)
		}
	}
	return result
}

var ipv6AddressRe = regexp.MustCompile(`\b[0-9a-fA-F]{1,4}(?::[0-9a-fA-F]{1,4}){2,}\b|\b[0-9a-fA-F:]*::[0-9a-fA-F:]*\b`)

func extractIPv6Addresses(text string) []string {
	var result []string
	seen := make(map[string]bool)
	for _, m := range ipv6AddressRe.FindAllString(text, -1) {
		if !seen[m] {
			seen[m] = true
			result = append(result, m)
		}
	}
	return result
}

// ApplySystemDNS sets static DNS servers on the designated adapter.
func ApplySystemDNS(adapterName, ipv4Primary, ipv4Secondary, ipv6Primary, ipv6Secondary string) error {
	if strings.TrimSpace(adapterName) == "" {
		return fmt.Errorf("ağ adaptörü seçilmedi")
	}
	if strings.TrimSpace(ipv4Primary) == "" {
		return fmt.Errorf("birincil DNS adresi belirtilmedi")
	}

	// 1. First backup current state
	isDHCP, currentIPv4, currentIPv6, _ := GetAdapterCurrentDNS(adapterName)
	_ = SaveBackup(DNSBackup{
		AdapterName: adapterName,
		IsDHCP:      isDHCP,
		IPv4Servers: currentIPv4,
		IPv6Servers: currentIPv6,
	})

	// 2. Set primary IPv4 DNS
	cmd1 := exec.Command("netsh", "interface", "ipv4", "set", "dnsservers",
		fmt.Sprintf("name=%s", adapterName), "static", ipv4Primary, "primary")
	cmd1.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var out1 bytes.Buffer
	cmd1.Stdout = &out1
	cmd1.Stderr = &out1
	if err := cmd1.Run(); err != nil {
		return fmt.Errorf("birincil DNS uygulanamadı (Yönetici izni gerekebilir): %s", out1.String())
	}

	// 3. Add secondary IPv4 DNS if provided
	if strings.TrimSpace(ipv4Secondary) != "" {
		cmd2 := exec.Command("netsh", "interface", "ipv4", "add", "dnsservers",
			fmt.Sprintf("name=%s", adapterName), ipv4Secondary, "index=2")
		cmd2.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = cmd2.Run()
	}

	// 4. Set IPv6 if provided
	if strings.TrimSpace(ipv6Primary) != "" {
		cmd6 := exec.Command("netsh", "interface", "ipv6", "set", "dnsservers",
			fmt.Sprintf("name=%s", adapterName), "static", ipv6Primary, "primary")
		cmd6.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_ = cmd6.Run()

		if strings.TrimSpace(ipv6Secondary) != "" {
			cmd6Sec := exec.Command("netsh", "interface", "ipv6", "add", "dnsservers",
				fmt.Sprintf("name=%s", adapterName), ipv6Secondary, "index=2")
			cmd6Sec.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
			_ = cmd6Sec.Run()
		}
	}

	// 5. Flush local DNS cache
	_ = FlushDNSCache()
	return nil
}

// ResetToDHCP restores the adapter to automatically acquire DNS via DHCP.
func ResetToDHCP(adapterName string) error {
	if strings.TrimSpace(adapterName) == "" {
		return fmt.Errorf("ağ adaptörü belirtilmedi")
	}

	cmd4 := exec.Command("netsh", "interface", "ipv4", "set", "dnsservers",
		fmt.Sprintf("name=%s", adapterName), "source=dhcp")
	cmd4.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	var out bytes.Buffer
	cmd4.Stdout = &out
	cmd4.Stderr = &out
	if err := cmd4.Run(); err != nil {
		return fmt.Errorf("IPv4 DHCP ayarlanamadı: %s", out.String())
	}

	cmd6 := exec.Command("netsh", "interface", "ipv6", "set", "dnsservers",
		fmt.Sprintf("name=%s", adapterName), "source=dhcp")
	cmd6.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd6.Run()

	_ = FlushDNSCache()
	return nil
}

// RestoreFromLatestBackup restores DNS to the previously backed up state.
func RestoreFromLatestBackup() error {
	backup, err := LoadLatestBackup()
	if err != nil {
		return err
	}

	if backup.IsDHCP || len(backup.IPv4Servers) == 0 {
		return ResetToDHCP(backup.AdapterName)
	}

	primary := backup.IPv4Servers[0]
	secondary := ""
	if len(backup.IPv4Servers) > 1 {
		secondary = backup.IPv4Servers[1]
	}

	primary6 := ""
	secondary6 := ""
	if len(backup.IPv6Servers) > 0 {
		primary6 = backup.IPv6Servers[0]
	}
	if len(backup.IPv6Servers) > 1 {
		secondary6 = backup.IPv6Servers[1]
	}

	return ApplySystemDNS(backup.AdapterName, primary, secondary, primary6, secondary6)
}
