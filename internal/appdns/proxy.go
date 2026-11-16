package appdns

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	modIphlpapi             = syscall.NewLazyDLL("iphlpapi.dll")
	procGetExtendedUdpTable = modIphlpapi.NewProc("GetExtendedUdpTable")
	procOpenProcess         = syscall.NewLazyDLL("kernel32.dll").NewProc("OpenProcess")
	procCloseHandle         = syscall.NewLazyDLL("kernel32.dll").NewProc("CloseHandle")
	procQueryFullProcess    = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")
)

const (
	AF_INET                       = 2
	UDP_TABLE_OWNER_PID           = 1
	PROCESS_QUERY_LIMITED_INFO    = 0x1000
)

type MIB_UDPROW_OWNER_PID struct {
	LocalAddr uint32
	LocalPort uint32
	OwningPid uint32
}

type LocalDNSProxy struct {
	mu           sync.Mutex
	conn         *net.UDPConn
	running      bool
	defaultDNS   string
	stopChan     chan struct{}
	lastPidMap   map[uint16]uint32
	pidCache     map[uint32]string
	lastTableGet time.Time
}

var globalProxy = &LocalDNSProxy{
	defaultDNS: "1.1.1.1",
	lastPidMap: make(map[uint16]uint32),
	pidCache:   make(map[uint32]string),
}

func GetProxy() *LocalDNSProxy {
	return globalProxy
}

func (p *LocalDNSProxy) SetDefaultDNS(dnsIP string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.defaultDNS = dnsIP
}

func (p *LocalDNSProxy) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

func getPIDForUDPPort(port uint16) uint32 {
	var size uint32
	// Query buffer size first
	ret, _, _ := procGetExtendedUdpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, AF_INET, UDP_TABLE_OWNER_PID, 0)
	if size == 0 {
		return 0
	}

	buf := make([]byte, size)
	ret, _, _ = procGetExtendedUdpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, AF_INET, UDP_TABLE_OWNER_PID, 0)
	if ret != 0 {
		return 0
	}

	numEntries := *(*uint32)(unsafe.Pointer(&buf[0]))
	if numEntries == 0 {
		return 0
	}

	rowSize := unsafe.Sizeof(MIB_UDPROW_OWNER_PID{})
	rowsStart := unsafe.Pointer(uintptr(unsafe.Pointer(&buf[0])) + unsafe.Sizeof(uint32(0)))

	for i := uint32(0); i < numEntries; i++ {
		row := (*MIB_UDPROW_OWNER_PID)(unsafe.Pointer(uintptr(rowsStart) + uintptr(i)*rowSize))
		// LocalPort in MIB is network byte order
		rowPort := uint16((row.LocalPort>>8)&0xFF) | uint16((row.LocalPort&0xFF)<<8)
		if rowPort == port {
			return row.OwningPid
		}
	}
	return 0
}

func getProcessNameFromPID(pid uint32) string {
	if pid == 0 {
		return ""
	}
	hProc, _, _ := procOpenProcess.Call(PROCESS_QUERY_LIMITED_INFO, 0, uintptr(pid))
	if hProc == 0 {
		return ""
	}
	defer procCloseHandle.Call(hProc)

	var buf [1024]uint16
	size := uint32(len(buf))
	ret, _, _ := procQueryFullProcess.Call(hProc, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if ret != 0 && size > 0 {
		fullPath := syscall.UTF16ToString(buf[:size])
		return filepath.Base(fullPath)
	}
	return ""
}

func (p *LocalDNSProxy) Start(listenAddr string) error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return nil
	}

	addr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		p.mu.Unlock()
		return err
	}

	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("yerel DNS Proxy başlatılamadı: %w", err)
	}

	p.conn = conn
	p.running = true
	p.stopChan = make(chan struct{})
	p.mu.Unlock()

	go p.serveLoop()
	return nil
}

func (p *LocalDNSProxy) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return
	}
	p.running = false
	if p.conn != nil {
		_ = p.conn.Close()
	}
	close(p.stopChan)
}

func (p *LocalDNSProxy) serveLoop() {
	buffer := make([]byte, 4096)
	for {
		n, clientAddr, err := p.conn.ReadFromUDP(buffer)
		if err != nil {
			select {
			case <-p.stopChan:
				return
			default:
				time.Sleep(10 * time.Millisecond)
				continue
			}
		}

		reqData := make([]byte, n)
		copy(reqData, buffer[:n])

		go func(data []byte, cAddr *net.UDPAddr) {
			// Find PID from client port
			pid := getPIDForUDPPort(uint16(cAddr.Port))
			procName := ""
			if pid > 0 {
				procName = getProcessNameFromPID(pid)
			}

			// Check if rule matches
			targetDNS := p.defaultDNS
			if procName != "" {
				rule := FindRuleForProcess(procName)
				if rule != nil && rule.DNSPrimary != "" {
					targetDNS = rule.DNSPrimary
				}
			}

			// Forward DNS query to targetDNS:53
			respData, err := forwardDNSQuery(targetDNS, data)
			if err == nil && len(respData) > 0 {
				_, _ = p.conn.WriteToUDP(respData, cAddr)
			}
		}(reqData, clientAddr)
	}
}

func forwardDNSQuery(dnsServer string, query []byte) ([]byte, error) {
	if !strings.Contains(dnsServer, ":") {
		dnsServer = net.JoinHostPort(dnsServer, "53")
	}
	conn, err := net.DialTimeout("udp", dnsServer, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}

	resp := make([]byte, 4096)
	n, err := conn.Read(resp)
	if err != nil {
		return nil, err
	}
	return resp[:n], nil
}
