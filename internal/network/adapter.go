package network

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"syscall"
)

type AdapterInfo struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	InterfaceID  int      `json:"interfaceId"`
	Type         string   `json:"type"` // "Ethernet", "Wi-Fi", "Other"
	Status       string   `json:"status"` // "Up", "Down"
	MACAddress   string   `json:"macAddress"`
	IPv4         string   `json:"ipv4"`
	IPv6         string   `json:"ipv6"`
	Gateway      string   `json:"gateway"`
	DNSServers   []string `json:"dnsServers"`
	LinkSpeed    string   `json:"linkSpeed"`
	IsActive     bool     `json:"isActive"`
}

// GetNetworkAdapters lists all network adapters with their IP, MAC, Gateway, and DNS configurations.
func GetNetworkAdapters() ([]AdapterInfo, error) {
	// First gather basic interfaces from Go standard library
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	// Use powershell query for enriched Windows network adapter info (Gateway, DNS, Description, LinkSpeed)
	psScript := `Get-NetAdapter | Where-Object { $_.Status -eq 'Up' -or $_.Status -eq 'Disconnected' } | ForEach-Object {
		$adapter = $_
		$ipConfig = Get-NetIPConfiguration -InterfaceIndex $adapter.InterfaceIndex -ErrorAction SilentlyContinue
		$dnsServers = @()
		if ($ipConfig.DNSServer) {
			$dnsServers = $ipConfig.DNSServer.ServerAddresses
		}
		$gateways = @()
		if ($ipConfig.IPv4DefaultGateway) {
			$gateways += $ipConfig.IPv4DefaultGateway.NextHop
		}
		[PSCustomObject]@{
			Name = $adapter.Name
			Description = $adapter.InterfaceDescription
			InterfaceIndex = $adapter.InterfaceIndex
			Type = if ($adapter.Name -like "*Wi-Fi*" -or $adapter.InterfaceDescription -like "*Wireless*" -or $adapter.InterfaceDescription -like "*Wi-Fi*") { "Wi-Fi" } else { "Ethernet" }
			Status = if ($adapter.Status -eq 'Up') { "Up" } else { "Down" }
			MacAddress = $adapter.MacAddress
			LinkSpeed = $adapter.LinkSpeed
			Gateways = $gateways
			DnsServers = $dnsServers
		}
	} | ConvertTo-Json -Compress`

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()

	var result []AdapterInfo
	if err == nil && len(strings.TrimSpace(string(out))) > 0 {
		outStr := strings.TrimSpace(string(out))
		if strings.HasPrefix(outStr, "[") {
			var rawList []struct {
				Name           string   `json:"Name"`
				Description    string   `json:"Description"`
				InterfaceIndex int      `json:"InterfaceIndex"`
				Type           string   `json:"Type"`
				Status         string   `json:"Status"`
				MacAddress     string   `json:"MacAddress"`
				LinkSpeed      string   `json:"LinkSpeed"`
				Gateways       []string `json:"Gateways"`
				DnsServers     []string `json:"DnsServers"`
			}
			if err := json.Unmarshal([]byte(outStr), &rawList); err == nil {
				for _, r := range rawList {
					info := AdapterInfo{
						Name:        r.Name,
						Description: r.Description,
						InterfaceID: r.InterfaceIndex,
						Type:        r.Type,
						Status:      r.Status,
						MACAddress:  r.MacAddress,
						LinkSpeed:   r.LinkSpeed,
						DNSServers:  r.DnsServers,
						IsActive:    r.Status == "Up",
					}
					if len(r.Gateways) > 0 {
						info.Gateway = r.Gateways[0]
					}
					// Find IPs from Go interfaces
					for _, iface := range ifaces {
						if iface.Index == r.InterfaceIndex || strings.EqualFold(iface.Name, r.Name) {
							addrs, _ := iface.Addrs()
							for _, addr := range addrs {
								ipNet, ok := addr.(*net.IPNet)
								if !ok || ipNet.IP.IsLoopback() {
									continue
								}
								if ip4 := ipNet.IP.To4(); ip4 != nil {
									if info.IPv4 == "" {
										info.IPv4 = ip4.String()
									}
								} else if ip6 := ipNet.IP.To16(); ip6 != nil && !ipNet.IP.IsLinkLocalUnicast() {
									if info.IPv6 == "" {
										info.IPv6 = ip6.String()
									}
								}
							}
						}
					}
					result = append(result, info)
				}
			}
		} else if strings.HasPrefix(outStr, "{") {
			// Single item
			var r struct {
				Name           string   `json:"Name"`
				Description    string   `json:"Description"`
				InterfaceIndex int      `json:"InterfaceIndex"`
				Type           string   `json:"Type"`
				Status         string   `json:"Status"`
				MacAddress     string   `json:"MacAddress"`
				LinkSpeed      string   `json:"LinkSpeed"`
				Gateways       []string `json:"Gateways"`
				DnsServers     []string `json:"DnsServers"`
			}
			if err := json.Unmarshal([]byte(outStr), &r); err == nil {
				info := AdapterInfo{
					Name:        r.Name,
					Description: r.Description,
					InterfaceID: r.InterfaceIndex,
					Type:        r.Type,
					Status:      r.Status,
					MACAddress:  r.MacAddress,
					LinkSpeed:   r.LinkSpeed,
					DNSServers:  r.DnsServers,
					IsActive:    r.Status == "Up",
				}
				if len(r.Gateways) > 0 {
					info.Gateway = r.Gateways[0]
				}
				for _, iface := range ifaces {
					if iface.Index == r.InterfaceIndex || strings.EqualFold(iface.Name, r.Name) {
						addrs, _ := iface.Addrs()
						for _, addr := range addrs {
							ipNet, ok := addr.(*net.IPNet)
							if !ok || ipNet.IP.IsLoopback() {
								continue
							}
							if ip4 := ipNet.IP.To4(); ip4 != nil {
								if info.IPv4 == "" {
									info.IPv4 = ip4.String()
								}
							} else if ip6 := ipNet.IP.To16(); ip6 != nil && !ipNet.IP.IsLinkLocalUnicast() {
								if info.IPv6 == "" {
									info.IPv6 = ip6.String()
								}
							}
						}
					}
				}
				result = append(result, info)
			}
		}
	}

	// Fallback to basic Go net.Interfaces if powershell failed
	if len(result) == 0 {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			var ipv4, ipv6 string
			for _, addr := range addrs {
				ipNet, ok := addr.(*net.IPNet)
				if !ok || ipNet.IP.IsLoopback() {
					continue
				}
				if ip4 := ipNet.IP.To4(); ip4 != nil {
					ipv4 = ip4.String()
				} else if ip6 := ipNet.IP.To16(); ip6 != nil && !ipNet.IP.IsLinkLocalUnicast() {
					ipv6 = ip6.String()
				}
			}
			if ipv4 != "" {
				result = append(result, AdapterInfo{
					Name:        iface.Name,
					Description: iface.Name,
					InterfaceID: iface.Index,
					Type:        "Ethernet",
					Status:      "Up",
					MACAddress:  iface.HardwareAddr.String(),
					IPv4:        ipv4,
					IPv6:        ipv6,
					IsActive:    true,
				})
			}
		}
	}

	return result, nil
}

// GetPrimaryActiveAdapter returns the active adapter having internet connectivity / default gateway.
func GetPrimaryActiveAdapter() (*AdapterInfo, error) {
	adapters, err := GetNetworkAdapters()
	if err != nil {
		return nil, err
	}
	for _, a := range adapters {
		if a.IsActive && a.IPv4 != "" && a.Gateway != "" {
			return &a, nil
		}
	}
	for _, a := range adapters {
		if a.IsActive && a.IPv4 != "" {
			return &a, nil
		}
	}
	if len(adapters) > 0 {
		return &adapters[0], nil
	}
	return nil, fmt.Errorf("aktif ağ adaptörü bulunamadı")
}
