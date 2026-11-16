package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"nexdns/internal/app"
	"nexdns/internal/appdns"
	"nexdns/internal/diagnostics"
	"nexdns/internal/discord"
	"nexdns/internal/dns"
	"nexdns/internal/network"
	"nexdns/internal/settings"
	"nexdns/internal/traffic"
	"nexdns/ui"
)

func isElevated() bool {
	var token windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token)
	if err != nil {
		return false
	}
	defer token.Close()
	return token.IsElevated()
}

func relaunchAsAdmin() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	var args string
	if len(os.Args) > 1 {
		args = strings.Join(os.Args[1:], " ")
	}
	verbPtr, _ := windows.UTF16PtrFromString("runas")
	exePtr, _ := windows.UTF16PtrFromString(exe)
	cwdPtr, _ := windows.UTF16PtrFromString(cwd)
	var argPtr *uint16
	if args != "" {
		argPtr, _ = windows.UTF16PtrFromString(args)
	}
	return windows.ShellExecute(0, verbPtr, exePtr, argPtr, cwdPtr, windows.SW_SHOWNORMAL)
}

func main() {
	if !isElevated() {
		if err := relaunchAsAdmin(); err == nil {
			return
		}
	}

	instanceMutex, alreadyRunning, err := acquireSingleInstance()
	if err != nil {
		log.Fatalf("NEXDNS instance lock: %v", err)
	}
	if alreadyRunning {
		_ = windows.CloseHandle(instanceMutex)
		activateExistingInstance()
		return
	}
	defer windows.CloseHandle(instanceMutex)

	// Initialize core controller
	controller := app.GetController()
	if err := controller.Init(); err != nil {
		log.Printf("Controller initialization warning: %v", err)
	}
	if err := setStartupRegistration(controller.GetSettings().AutoStart); err != nil {
		log.Printf("Windows startup registration warning: %v", err)
	}
	setTrayMode(controller.GetSettings().Mode)
	go func() {
		for {
			time.Sleep(2 * time.Second)
			state := controller.GetDashboardState()
			setTrayDNSStatus(state.DNSLatencyMs)
		}
	}()
	var modeChangeMu sync.Mutex
	trayModeSetter = func(mode string) error {
		modeChangeMu.Lock()
		defer modeChangeMu.Unlock()
		if err := controller.SwitchMode(mode); err != nil {
			return err
		}
		setTrayMode(mode)
		return nil
	}

	// Prepare data directory for WebView2 runtime cache
	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = "."
	}
	cacheDir := filepath.Join(appData, "NEXDNS", "cache")
	_ = os.MkdirAll(cacheDir, 0755)

	// Create native Win32 window with embedded Microsoft Edge WebView2
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:    false,
		DataPath: cacheDir,
		WindowOptions: webview2.WindowOptions{
			Title:  "NEXDNS — Advanced Network & DNS Manager",
			Width:  1280,
			Height: 820,
			Center: true,
		},
	})
	if w == nil {
		log.Fatal("WebView2 runtime başlatılamadı. Sistemde Microsoft Edge WebView2 kurulu olduğundan emin olun.")
	}
	defer w.Destroy()

	hwnd := uintptr(w.Window())
	bg, brd, primary, accent := themeColors(controller.GetSettings().Theme)
	setTrayTheme(bg, brd, primary, accent)
	if err := installBrandIcons(hwnd); err != nil {
		log.Printf("Application icon setup warning: %v", err)
	} else {
		defer destroyCurrentWindowIcon()
	}
	if err := installSystemTray(hwnd); err != nil {
		log.Printf("System tray setup warning: %v", err)
	} else {
		defer cleanupSystemTray(hwnd)
		if hasBackgroundArgument() {
			showWindow(hwnd, swHide)
		}
	}

	// -------------------------------------------------------------------------
	// BIND GO FUNCTIONS DIRECTLY TO JAVASCRIPT GLOBAL SCOPE
	// -------------------------------------------------------------------------

	// Dashboard & General
	_ = w.Bind("GetDashboardState", func() (app.DashboardState, error) {
		return controller.GetDashboardState(), nil
	})
	_ = w.Bind("SwitchMode", func(mode string) error {
		return trayModeSetter(mode)
	})

	// DNS Management
	_ = w.Bind("GetProviders", func() ([]dns.Provider, error) {
		return controller.GetProviders(), nil
	})
	_ = w.Bind("MeasureProviders", func() ([]dns.Provider, error) {
		return controller.MeasureProviders(), nil
	})
	_ = w.Bind("MeasureProvider", func(id string) (dns.Provider, error) {
		return controller.MeasureProvider(id), nil
	})
	_ = w.Bind("ApplyProvider", func(id string) error {
		return controller.ApplyProvider(id)
	})
	_ = w.Bind("ApplyCustomDNS", func(name, p4, s4, p6, s6 string) error {
		return controller.ApplyCustomDNS(name, p4, s4, p6, s6)
	})
	_ = w.Bind("ResetToDHCP", func() error {
		return controller.ResetToDHCP()
	})
	_ = w.Bind("RestoreBackup", func() error {
		return controller.RestoreBackup()
	})
	_ = w.Bind("FlushDNS", func() error {
		return controller.FlushDNS()
	})

	// Network & Traffic
	_ = w.Bind("GetAdapters", func() ([]network.AdapterInfo, error) {
		return controller.GetAdapters()
	})
	_ = w.Bind("GetTrafficStats", func() (traffic.TrafficSnapshot, error) {
		return controller.GetTrafficStats(), nil
	})
	_ = w.Bind("GetTrafficHistory", func() ([]traffic.TrafficSnapshot, error) {
		return controller.GetTrafficHistory(), nil
	})

	// Diagnostics & Discord
	_ = w.Bind("RunDiscordDiagnostics", func() (discord.DiscordDiagnosticResult, error) {
		return controller.RunDiscordDiagnostics(), nil
	})
	_ = w.Bind("RunNetworkDiagnostics", func() (diagnostics.DiagnosticsSummary, error) {
		return controller.RunNetworkDiagnostics(), nil
	})

	// Application Rules & Processes
	_ = w.Bind("GetRunningApps", func() ([]appdns.RunningProcess, error) {
		return controller.GetRunningApps()
	})
	_ = w.Bind("GetAppRules", func() ([]appdns.AppRule, error) {
		return controller.GetAppRules(), nil
	})
	_ = w.Bind("AddAppRule", func(ruleJSON string) error {
		return controller.AddAppRule(ruleJSON)
	})
	_ = w.Bind("DeleteAppRule", func(id string) error {
		return controller.DeleteAppRule(id)
	})
	_ = w.Bind("ToggleAppRule", func(id string, enabled bool) error {
		return controller.ToggleAppRule(id, enabled)
	})
	_ = w.Bind("OpenFilePicker", func() (string, error) {
		return controller.OpenFilePicker()
	})

	// Settings
	_ = w.Bind("GetSettings", func() (*settings.Config, error) {
		return controller.GetSettings(), nil
	})
	_ = w.Bind("SaveSettings", func(cfgJSON string) error {
		if err := controller.SaveSettings(cfgJSON); err != nil {
			return err
		}
		return setStartupRegistration(controller.GetSettings().AutoStart)
	})
	_ = w.Bind("SetWindowTheme", func(background, foreground, border, primary, accent string) error {
		return setWindowTheme(w.Window(), background, foreground, border, primary, accent)
	})

	// Load bundled self-contained HTML (zero external web server, zero port binding)
	html, err := ui.GetBundledHTML()
	if err != nil {
		w.SetHtml(fmt.Sprintf("<html><body><h1>Hata</h1><p>%v</p></body></html>", err))
	} else {
		w.SetHtml(html)
	}

	// Run native Windows message loop
	w.Run()
}

func setWindowTheme(hwnd unsafe.Pointer, background, foreground, border, primary, accent string) error {
	backgroundColor, err := colorRef(background)
	if err != nil {
		return err
	}
	foregroundColor, err := colorRef(foreground)
	if err != nil {
		return err
	}
	borderColor, err := colorRef(border)
	if err != nil {
		return err
	}

	window := windows.HWND(uintptr(hwnd))
	colorSize := uint32(unsafe.Sizeof(uint32(0)))
	attributes := []struct {
		id    uint32
		color uint32
	}{
		{windows.DWMWA_CAPTION_COLOR, backgroundColor},
		{windows.DWMWA_TEXT_COLOR, foregroundColor},
		{windows.DWMWA_BORDER_COLOR, borderColor},
	}
	for _, attribute := range attributes {
		if err := windows.DwmSetWindowAttribute(window, attribute.id, unsafe.Pointer(&attribute.color), colorSize); err != nil {
			return fmt.Errorf("window theme attribute %d: %w", attribute.id, err)
		}
	}
	setTrayTheme(background, border, primary, accent)
	return nil
}

func themeColors(theme string) (background, border, primary, accent string) {
	switch theme {
	case "cyber-neon":
		return "#0a1122", "#1b325c", "#06b6d4", "#3b82f6"
	case "obsidian-black":
		return "#0f172a", "#1e293b", "#f1f5f9", "#94a3b8"
	case "arctic-blue":
		return "#0b192c", "#1e3a5f", "#38bdf8", "#60a5fa"
	case "emerald-pro":
		return "#061a14", "#0f382a", "#10b981", "#34d399"
	case "crimson-elite":
		return "#1a0808", "#3b1414", "#ef4444", "#f87171"
	case "light-professional":
		return "#f1f5f9", "#cbd5e1", "#2563eb", "#3b82f6"
	case "graphite-aurora":
		return "#121118", "#29243b", "#7c3aed", "#22d3ee"
	default: // midnight-purple
		return "#11111b", "#2b2b42", "#8b5cf6", "#a855f7"
	}
}

func themeBrandColors(theme string) (string, string) {
	switch theme {
	case "cyber-neon":
		return "#06b6d4", "#3b82f6"
	case "obsidian-black":
		return "#f1f5f9", "#94a3b8"
	case "arctic-blue":
		return "#38bdf8", "#60a5fa"
	case "emerald-pro":
		return "#10b981", "#34d399"
	case "crimson-elite":
		return "#ef4444", "#f87171"
	case "light-professional":
		return "#2563eb", "#3b82f6"
	case "graphite-aurora":
		return "#7c3aed", "#22d3ee"
	default:
		return "#8b5cf6", "#a855f7"
	}
}

func colorRef(value string) (uint32, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 {
		return 0, fmt.Errorf("invalid RGB color %q", value)
	}

	rgb, err := strconv.ParseUint(value, 16, 24)
	if err != nil {
		return 0, fmt.Errorf("invalid RGB color %q: %w", value, err)
	}
	return uint32((rgb&0x0000ff)<<16 | (rgb & 0x00ff00) | ((rgb & 0xff0000) >> 16)), nil
}
