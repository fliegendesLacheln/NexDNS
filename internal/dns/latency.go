package dns

import (
	"crypto/rand"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// buildDnsQueryPacket constructs a standard RFC 1035 DNS A-query packet for a domain.
func buildDnsQueryPacket(domain string) ([]byte, uint16, error) {
	var idBytes [2]byte
	_, _ = rand.Read(idBytes[:])
	txID := uint16(idBytes[0])<<8 | uint16(idBytes[1])

	// Header: 12 bytes
	header := []byte{
		byte(txID >> 8), byte(txID & 0xFF), // Transaction ID
		0x01, 0x00,                         // Flags: Standard query, Recursion Desired
		0x00, 0x01,                         // Questions: 1
		0x00, 0x00,                         // Answer RRs: 0
		0x00, 0x00,                         // Authority RRs: 0
		0x00, 0x00,                         // Additional RRs: 0
	}

	// Question section: labels of domain
	parts := strings.Split(domain, ".")
	var qname []byte
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		if len(part) > 63 {
			return nil, 0, fmt.Errorf("domain label too long: %s", part)
		}
		qname = append(qname, byte(len(part)))
		qname = append(qname, []byte(part)...)
	}
	qname = append(qname, 0x00) // null terminator

	// Type A (0x0001), Class IN (0x0001)
	qtail := []byte{0x00, 0x01, 0x00, 0x01}

	packet := append(header, qname...)
	packet = append(packet, qtail...)
	return packet, txID, nil
}

// MeasureDNSLatency sends a real RFC 1035 DNS query to the specified DNS server IP.
// Returns round-trip time in milliseconds, or -1 if unreachable/timeout.
func MeasureDNSLatency(serverIP string, timeout time.Duration) (int64, error) {
	if strings.TrimSpace(serverIP) == "" {
		return -1, fmt.Errorf("empty server address")
	}

	// Handle IPv6 bracket formatting if needed
	targetAddr := net.JoinHostPort(serverIP, "53")
	conn, err := net.DialTimeout("udp", targetAddr, timeout)
	if err != nil {
		return -1, err
	}
	defer conn.Close()

	packet, txID, err := buildDnsQueryPacket("cloudflare.com")
	if err != nil {
		return -1, err
	}

	start := time.Now()
	_ = conn.SetDeadline(start.Add(timeout))

	if _, err := conn.Write(packet); err != nil {
		return -1, err
	}

	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		return -1, err
	}
	duration := time.Since(start).Milliseconds()

	if n < 12 {
		return -1, fmt.Errorf("response packet too short")
	}

	respID := uint16(buf[0])<<8 | uint16(buf[1])
	if respID != txID {
		return -1, fmt.Errorf("transaction ID mismatch")
	}

	if duration < 1 {
		duration = 1
	}
	return duration, nil
}

// MeasureSingleProvider measures latency for a single DNS provider.
func MeasureSingleProvider(p Provider) Provider {
	lat, err := MeasureDNSLatency(p.IPv4Primary, 2*time.Second)
	if err != nil || lat < 0 {
		p.LatencyMs = -1
		p.Status = "offline"
	} else {
		p.LatencyMs = lat
		switch {
		case lat < 30:
			p.Status = "online"
		case lat < 100:
			p.Status = "good"
		default:
			p.Status = "slow"
		}
	}
	return p
}

// MeasureAllProviders checks latency for all provided DNS providers concurrently.
func MeasureAllProviders(providers []Provider) []Provider {
	var wg sync.WaitGroup
	result := make([]Provider, len(providers))
	copy(result, providers)

	for i := range result {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result[idx] = MeasureSingleProvider(result[idx])
		}(i)
	}

	wg.Wait()
	return result
}
