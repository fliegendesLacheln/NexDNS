package main

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	wmClose          = 0x0010
	wmCommand        = 0x0111
	wmSetIcon        = 0x0080
	wmAppActivate    = wmApp + 72
	wmTrayShowMenu   = wmApp + 73
	wmDrawItem       = 0x002B
	wmMeasureItem    = 0x002C
	wmApp            = 0x8000
	wmTrayCallback   = wmApp + 71
	wmContextMenu    = 0x007B
	iconSmall        = 0
	iconBig          = 1
	gwlpWndProc      = ^uintptr(3)
	trayIconID       = 1
	nimAdd           = 0
	nimModify        = 1
	nimDelete        = 2
	nifMessage       = 0x00000001
	nifIcon          = 0x00000002
	nifTip           = 0x00000004
	wmLButtonDblClk  = 0x0203
	wmRButtonUp      = 0x0205
	swHide           = 0
	swShow           = 5
	swRestore        = 9
	mfString         = 0x0000
	mfSeparator      = 0x0800
	mfOwnerDraw      = 0x0100
	tpmReturnCommand = 0x0100
	tpmNoNotify      = 0x0080
	menuRestore      = 4101
	menuModeToggle   = 4102
	menuExit         = 4103
	odtMenu          = 1
	psSolid          = 0
	transparent      = 1
	dtSingleLine     = 0x0020
	dtVCenter        = 0x0004
	colorMenu        = 4
	colorMenuText    = 7
	dibRGBColors     = 0

	// Flyout dimensions
	flyoutWidth  = 300
	flyoutHeight = 240
)

type notifyIconData struct {
	cbSize           uint32
	hWnd             windows.HWND
	uid              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            windows.Handle
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uTimeout         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         windows.GUID
	hBalloonIcon     windows.Handle
}

type point struct {
	x int32
	y int32
}

type rectangle struct {
	left   int32
	top    int32
	right  int32
	bottom int32
}

type paintStruct struct {
	hdc         windows.Handle
	fErase      int32
	rcPaint     rectangle
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

type measureItemStruct struct {
	controlType uint32
	controlID   uint32
	itemID      uint32
	itemWidth   uint32
	itemHeight  uint32
	itemData    uintptr
}

type drawItemStruct struct {
	controlType uint32
	controlID   uint32
	itemID      uint32
	itemAction  uint32
	itemState   uint32
	window      windows.HWND
	device      windows.Handle
	itemRect    rectangle
	itemData    uintptr
}

type bitmapInfoHeader struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
}

type bitmapInfo struct {
	header bitmapInfoHeader
	colors [1]uint32
}

type iconInfo struct {
	icon     uint32
	xHotspot uint32
	yHotspot uint32
	mask     windows.Handle
	color    windows.Handle
}

//go:embed assets/nexdns.ico
var embeddedBrandIcon []byte

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	shell32                = windows.NewLazySystemDLL("shell32.dll")
	gdi32                  = windows.NewLazySystemDLL("gdi32.dll")
	kernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procSetWindowLongPtr   = user32.NewProc("SetWindowLongPtrW")
	procGetWindowLongPtr   = user32.NewProc("GetWindowLongPtrW")
	procCallWindowProc     = user32.NewProc("CallWindowProcW")
	procShowWindow         = user32.NewProc("ShowWindow")
	procSetForeground      = user32.NewProc("SetForegroundWindow")
	procGetCursorPos       = user32.NewProc("GetCursorPos")
	procFindWindow         = user32.NewProc("FindWindowW")
	procCreatePopupMenu    = user32.NewProc("CreatePopupMenu")
	procAppendMenu         = user32.NewProc("AppendMenuW")
	procTrackPopupMenu     = user32.NewProc("TrackPopupMenu")
	procDestroyMenu        = user32.NewProc("DestroyMenu")
	procPostMessage        = user32.NewProc("PostMessageW")
	procSendMessage        = user32.NewProc("SendMessageW")
	procCreateIconResource = user32.NewProc("CreateIconFromResourceEx")
	procCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	procCreatePen          = gdi32.NewProc("CreatePen")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procRoundRect          = gdi32.NewProc("RoundRect")
	procEllipse            = gdi32.NewProc("Ellipse")
	procFillRect           = user32.NewProc("FillRect")
	procDrawText           = user32.NewProc("DrawTextW")
	procSetTextColor       = gdi32.NewProc("SetTextColor")
	procSetBkMode          = gdi32.NewProc("SetBkMode")
	procGetSysColor        = user32.NewProc("GetSysColor")
	procGetSysColorBrush   = user32.NewProc("GetSysColorBrush")
	procDestroyIcon        = user32.NewProc("DestroyIcon")
	procShellNotifyIcon    = shell32.NewProc("Shell_NotifyIconW")
	procCreateMutex        = kernel32.NewProc("CreateMutexW")
	procCloseHandle        = kernel32.NewProc("CloseHandle")
	procDefWindowProc      = user32.NewProc("DefWindowProcW")
	procRegisterClassEx    = user32.NewProc("RegisterClassExW")
	procCreateWindowEx     = user32.NewProc("CreateWindowExW")
	procDestroyWindow      = user32.NewProc("DestroyWindow")
	procSetWindowPos       = user32.NewProc("SetWindowPos")
	procBeginPaint         = user32.NewProc("BeginPaint")
	procEndPaint           = user32.NewProc("EndPaint")
	procInvalidateRect     = user32.NewProc("InvalidateRect")
	procSetTimer           = user32.NewProc("SetTimer")
	procKillTimer          = user32.NewProc("KillTimer")
	procGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBmp = gdi32.NewProc("CreateCompatibleBitmap")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	trayData               notifyIconData
	windowProcOriginal     uintptr
	windowProcCallback     uintptr
	flyoutHwnd             uintptr
	flyoutCallback         uintptr
	pillAnimTarget         float64
	pillAnimPos            float64
	flyoutHover            int32
	flyoutFadeAlpha        int32
	flyoutFadeStep         int32
	trayInstalled          bool
	appWindow              uintptr
	currentSmallIcon       windows.Handle
	currentLargeIcon       windows.Handle
	exitRequested          atomic.Bool
	trayMenuOpen           atomic.Bool
	lastTrayMenuAt         time.Time
	trayMode               atomic.Uint32
	trayBackgroundColor    atomic.Uint32
	trayBorderColor        atomic.Uint32
	trayPrimaryColor       atomic.Uint32
	trayAccentColor        atomic.Uint32
	trayModeSetter         func(string) error
	trayDNSLatencyMs       atomic.Int64
	trayDNSConnected       atomic.Uint32
	trayProviderName       atomic.Value // stores string

	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procGetDC                      = user32.NewProc("GetDC")
	procReleaseDC                  = user32.NewProc("ReleaseDC")
	procUpdateLayeredWindow        = user32.NewProc("UpdateLayeredWindow")
	procCreateFont                 = gdi32.NewProc("CreateFontW")
	procSetWindowRgn               = user32.NewProc("SetWindowRgn")
	procCreateRoundRectRgn         = gdi32.NewProc("CreateRoundRectRgn")
	procGradientFill               = windows.NewLazySystemDLL("msimg32.dll").NewProc("GradientFill")
)

func setStartupRegistration(enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()

	if !enabled {
		err := key.DeleteValue("NEXDNS")
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return nil
		}
		return err
	}

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	command := fmt.Sprintf("\"%s\" --background", executable)
	return key.SetStringValue("NEXDNS", command)
}

func hasBackgroundArgument() bool {
	for _, arg := range os.Args[1:] {
		if strings.EqualFold(arg, "--background") {
			return true
		}
	}
	return false
}

func acquireSingleInstance() (windows.Handle, bool, error) {
	name, _ := windows.UTF16PtrFromString(`Local\NEXDNS.MainWindow.v2`)
	handle, _, callErr := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return 0, false, fmt.Errorf("create application instance lock: %v", callErr)
	}
	runtime.KeepAlive(name)
	return windows.Handle(handle), errors.Is(callErr, windows.ERROR_ALREADY_EXISTS), nil
}

func activateExistingInstance() {
	className, _ := windows.UTF16PtrFromString("webview")
	windowTitle, _ := windows.UTF16PtrFromString("NEXDNS — Advanced Network & DNS Manager")
	for attempt := 0; attempt < 30; attempt++ {
		hwnd, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windowTitle)))
		if hwnd != 0 {
			_, _, _ = procSendMessage.Call(hwnd, wmAppActivate, 0, 0)
			_, _, _ = procSetForeground.Call(hwnd)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	runtime.KeepAlive(className)
	runtime.KeepAlive(windowTitle)
}

func installBrandIcons(hwnd uintptr) error {
	small, err := createIconFromICO(16)
	if err != nil {
		return err
	}
	large, err := createIconFromICO(32)
	if err != nil {
		_, _, _ = procDestroyIcon.Call(uintptr(small))
		return err
	}
	currentSmallIcon = small
	currentLargeIcon = large
	if hwnd != 0 {
		_, _, _ = procSendMessage.Call(hwnd, wmSetIcon, iconSmall, uintptr(small))
		_, _, _ = procSendMessage.Call(hwnd, wmSetIcon, iconBig, uintptr(large))
	}
	return nil
}

func createIconFromICO(size int) (windows.Handle, error) {
	if len(embeddedBrandIcon) < 6 {
		return 0, fmt.Errorf("embedded NEXDNS icon is invalid")
	}
	count := int(binary.LittleEndian.Uint16(embeddedBrandIcon[4:6]))
	selectedOffset, selectedSize := -1, 0
	for i := 0; i < count; i++ {
		entry := 6 + i*16
		if entry+16 > len(embeddedBrandIcon) {
			break
		}
		width := int(embeddedBrandIcon[entry])
		if width == 0 {
			width = 256
		}
		if width != size {
			continue
		}
		selectedSize = int(binary.LittleEndian.Uint32(embeddedBrandIcon[entry+8 : entry+12]))
		selectedOffset = int(binary.LittleEndian.Uint32(embeddedBrandIcon[entry+12 : entry+16]))
		break
	}
	if selectedOffset < 0 || selectedSize <= 0 || selectedOffset+selectedSize > len(embeddedBrandIcon) {
		return 0, fmt.Errorf("NEXDNS %dpx icon is missing", size)
	}
	iconBits := append([]byte(nil), embeddedBrandIcon[selectedOffset:selectedOffset+selectedSize]...)
	icon, _, callErr := procCreateIconResource.Call(
		uintptr(unsafe.Pointer(&iconBits[0])), uintptr(len(iconBits)), 1, 0x00030000, uintptr(size), uintptr(size), 0,
	)
	runtime.KeepAlive(iconBits)
	if icon == 0 {
		return 0, fmt.Errorf("load NEXDNS %dpx icon: %v", size, callErr)
	}
	return windows.Handle(icon), nil
}

func setTrayTheme(background, border, primary, accent string) {
	if background != "" {
		if bgColor, err := parseHexColor(background); err == nil {
			trayBackgroundColor.Store(colorRefFromRGBA(bgColor))
		}
	}
	if border != "" {
		if brdColor, err := parseHexColor(border); err == nil {
			trayBorderColor.Store(colorRefFromRGBA(brdColor))
		}
	}
	if primary != "" {
		if primaryColor, err := parseHexColor(primary); err == nil {
			trayPrimaryColor.Store(colorRefFromRGBA(primaryColor))
		}
	}
	if accent != "" {
		if accentColor, err := parseHexColor(accent); err == nil {
			trayAccentColor.Store(colorRefFromRGBA(accentColor))
		}
	}
}

func setTrayMode(mode string) {
	if mode == "app" {
		trayMode.Store(1)
		return
	}
	trayMode.Store(0)
}

func setTrayDNSStatus(latencyMs int64) {
	trayDNSLatencyMs.Store(latencyMs)
	if flyoutHwnd != 0 {
		procInvalidateRect.Call(flyoutHwnd, 0, 0)
	}
}

func installSystemTray(hwnd uintptr) error {
	if hwnd == 0 {
		return fmt.Errorf("native window handle is unavailable")
	}
	appWindow = hwnd
	trayData = notifyIconData{
		cbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		hWnd:             windows.HWND(hwnd),
		uid:              trayIconID,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: wmTrayCallback,
		hIcon:            currentSmallIcon,
	}
	tip, _ := windows.UTF16FromString("NEXDNS")
	copy(trayData.szTip[:], tip)
	if result, _, callErr := procShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&trayData))); result == 0 {
		return fmt.Errorf("add tray icon: %w", callErr)
	}
	trayInstalled = true
	windowProcOriginal, _, _ = procGetWindowLongPtr.Call(hwnd, gwlpWndProc)
	if windowProcOriginal == 0 {
		cleanupSystemTray(hwnd)
		return fmt.Errorf("read native window procedure failed")
	}
	windowProcCallback = windows.NewCallback(trayWindowProcedure)
	previous, _, _ := procSetWindowLongPtr.Call(hwnd, gwlpWndProc, windowProcCallback)
	if previous == 0 {
		cleanupSystemTray(hwnd)
		return fmt.Errorf("install system tray window procedure failed")
	}
	initFlyoutWindow()
	return nil
}

func cleanupSystemTray(hwnd uintptr) {
	if flyoutHwnd != 0 {
		procDestroyWindow.Call(flyoutHwnd)
		flyoutHwnd = 0
	}
	if trayInstalled {
		_, _, _ = procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&trayData)))
		trayInstalled = false
	}
	if hwnd != 0 && windowProcOriginal != 0 {
		_, _, _ = procSetWindowLongPtr.Call(hwnd, gwlpWndProc, windowProcOriginal)
	}
	windowProcOriginal = 0
	windowProcCallback = 0
	appWindow = 0
}

func trayWindowProcedure(hwnd, message, wParam, lParam uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("trayWindowProcedure panic recovered: %v", r)
		}
	}()
	switch message {
	case wmClose:
		if exitRequested.Load() {
			if trayInstalled {
				_, _, _ = procShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&trayData)))
				trayInstalled = false
			}
			return callOriginalWindowProcedure(hwnd, message, wParam, lParam)
		}
		showWindow(hwnd, swHide)
		return 0
	case wmAppActivate:
		restoreMainWindow(hwnd)
		return 0
	case wmTrayCallback:
		switch lParam {
		case wmLButtonDblClk:
			restoreMainWindow(hwnd)
		case wmRButtonUp, wmContextMenu, 0x0202:
			showTrayFlyout()
		}
		return 0
	default:
		return callOriginalWindowProcedure(hwnd, message, wParam, lParam)
	}
}

func callOriginalWindowProcedure(hwnd, message, wParam, lParam uintptr) uintptr {
	result, _, _ := procCallWindowProc.Call(windowProcOriginal, hwnd, message, wParam, lParam)
	return result
}

func initFlyoutWindow() {
	if flyoutHwnd != 0 {
		return
	}
	className, _ := windows.UTF16PtrFromString("NEXDNS_TrayFlyout")
	flyoutCallback = windows.NewCallback(flyoutWndProc)
	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   flyoutCallback,
		lpszClassName: className,
	}
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))

	// WS_EX_TOPMOST | WS_EX_TOOLWINDOW | WS_EX_LAYERED
	h, _, _ := procCreateWindowEx.Call(
		0x00000008|0x00000080|0x00080000,
		uintptr(unsafe.Pointer(className)),
		0,
		0x80000000, // WS_POPUP
		0, 0, uintptr(flyoutWidth), uintptr(flyoutHeight),
		0, 0, 0, 0,
	)
	flyoutHwnd = h

	// Set window region for rounded corners
	rgn, _, _ := procCreateRoundRectRgn.Call(0, 0, flyoutWidth, flyoutHeight, 20, 20)
	if rgn != 0 {
		procSetWindowRgn.Call(flyoutHwnd, rgn, 1)
	}

	// Set layered window for transparency (fully opaque initially, used for fade animation)
	procSetLayeredWindowAttributes.Call(flyoutHwnd, 0, 255, 0x00000002) // LWA_ALPHA

	runtime.KeepAlive(className)
}

func showTrayFlyout() {
	if flyoutHwnd == 0 {
		initFlyoutWindow()
	}
	if flyoutHwnd == 0 {
		return
	}

	if trayMode.Load() == 0 {
		pillAnimTarget = 0.0
		pillAnimPos = 0.0
	} else {
		pillAnimTarget = 1.0
		pillAnimPos = 1.0
	}
	flyoutHover = 0
	flyoutFadeAlpha = 0
	flyoutFadeStep = 0

	var cursor point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor)))
	cx, _, _ := procGetSystemMetrics.Call(0)
	cy, _, _ := procGetSystemMetrics.Call(1)

	width := int32(flyoutWidth)
	height := int32(flyoutHeight)
	x := cursor.x - (width / 2)
	if x < 10 {
		x = 10
	}
	if x+width+10 > int32(cx) {
		x = int32(cx) - width - 10
	}
	y := cursor.y - height - 14
	if y < 10 {
		y = cursor.y + 24
	}
	if y+height+10 > int32(cy) {
		y = int32(cy) - height - 10
	}

	// Start fade-in: begin transparent
	procSetLayeredWindowAttributes.Call(flyoutHwnd, 0, 0, 0x00000002)
	procSetWindowPos.Call(flyoutHwnd, ^uintptr(0), uintptr(x), uintptr(y), uintptr(width), uintptr(height), 0x0040)
	procSetForeground.Call(flyoutHwnd)
	procInvalidateRect.Call(flyoutHwnd, 0, 0)

	// Start fade-in timer (timer ID 2)
	procSetTimer.Call(flyoutHwnd, 2, 12, 0)
}

// createFlyoutFont creates a font for the flyout with the given size and weight.
func createFlyoutFont(height int32, weight int32) uintptr {
	fontName, _ := windows.UTF16PtrFromString("Segoe UI")
	font, _, _ := procCreateFont.Call(
		uintptr(height), 0, 0, 0,
		uintptr(weight),
		0, 0, 0, // italic, underline, strikeout
		1,    // DEFAULT_CHARSET
		0,    // OUT_DEFAULT_PRECIS
		0,    // CLIP_DEFAULT_PRECIS
		5,    // CLEARTYPE_QUALITY
		0,    // DEFAULT_PITCH
		uintptr(unsafe.Pointer(fontName)),
	)
	runtime.KeepAlive(fontName)
	return font
}

// blendColor mixes two COLORREF values by alpha (0.0 = c1, 1.0 = c2).
func blendColor(c1, c2 uint32, alpha float64) uint32 {
	r1, g1, b1 := c1&0xFF, (c1>>8)&0xFF, (c1>>16)&0xFF
	r2, g2, b2 := c2&0xFF, (c2>>8)&0xFF, (c2>>16)&0xFF
	r := uint32(float64(r1)*(1-alpha) + float64(r2)*alpha)
	g := uint32(float64(g1)*(1-alpha) + float64(g2)*alpha)
	b := uint32(float64(b1)*(1-alpha) + float64(b2)*alpha)
	return r | (g << 8) | (b << 16)
}

// lightenColor makes a COLORREF brighter by the given amount (0.0-1.0).
func lightenColor(c uint32, amount float64) uint32 {
	return blendColor(c, 0x00FFFFFF, amount)
}

// darkenColor makes a COLORREF darker by the given amount (0.0-1.0).
func darkenColor(c uint32, amount float64) uint32 {
	return blendColor(c, 0x00000000, amount)
}

func flyoutWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case 0x0006: // WM_ACTIVATE
		if wParam&0xFFFF == 0 { // WA_INACTIVE
			// Start fade-out
			flyoutFadeAlpha = 255
			flyoutFadeStep = 1
			procSetTimer.Call(hwnd, 3, 10, 0) // timer ID 3 = fade out
			return 0
		}
	case 0x0113: // WM_TIMER
		switch wParam {
		case 1: // pill animation timer
			diff := pillAnimTarget - pillAnimPos
			if math.Abs(diff) < 0.02 {
				pillAnimPos = pillAnimTarget
				procKillTimer.Call(hwnd, 1)
			} else {
				pillAnimPos += diff * 0.3
			}
			procInvalidateRect.Call(hwnd, 0, 0)
		case 2: // fade-in timer
			flyoutFadeAlpha += 40
			if flyoutFadeAlpha >= 255 {
				flyoutFadeAlpha = 255
				procKillTimer.Call(hwnd, 2)
			}
			procSetLayeredWindowAttributes.Call(hwnd, 0, uintptr(flyoutFadeAlpha), 0x00000002)
		case 3: // fade-out timer
			flyoutFadeAlpha -= 45
			if flyoutFadeAlpha <= 0 {
				flyoutFadeAlpha = 0
				procKillTimer.Call(hwnd, 3)
				procShowWindow.Call(hwnd, 0) // SW_HIDE
			}
			procSetLayeredWindowAttributes.Call(hwnd, 0, uintptr(flyoutFadeAlpha), 0x00000002)
		}
		return 0
	case 0x0200: // WM_MOUSEMOVE
		mx := int32(lParam & 0xFFFF)
		my := int32((lParam >> 16) & 0xFFFF)
		var newHover int32
		// Bottom buttons region (Y: 168..222)
		if my >= 168 && my <= 222 {
			if mx >= 16 && mx <= 144 {
				newHover = 1 // "NEXDNS'e Dön"
			} else if mx >= 156 && mx <= 284 {
				newHover = 2 // "Çıkış"
			}
		}
		// Pill toggle region (Y: 80..124)
		if my >= 80 && my <= 124 && mx >= 16 && mx <= 284 {
			if mx < 150 {
				newHover = 3 // left pill
			} else {
				newHover = 4 // right pill
			}
		}
		if newHover != flyoutHover {
			flyoutHover = newHover
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case 0x02A3: // WM_MOUSELEAVE
		if flyoutHover != 0 {
			flyoutHover = 0
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case 0x0201: // WM_LBUTTONDOWN
		mx := int32(lParam & 0xFFFF)
		my := int32((lParam >> 16) & 0xFFFF)

		// Mode switch pill clicked (Y: 80..124)
		if my >= 80 && my <= 124 && mx >= 16 && mx <= 284 {
			newMode := "system"
			if mx >= 150 {
				newMode = "app"
				pillAnimTarget = 1.0
			} else {
				pillAnimTarget = 0.0
			}
			procSetTimer.Call(hwnd, 1, 16, 0)
			if trayModeSetter != nil {
				go func(m string) {
					_ = trayModeSetter(m)
				}(newMode)
			}
			return 0
		}
		// "NEXDNS'e Dön" clicked (Y: 168..222, X: 16..144)
		if my >= 168 && my <= 222 && mx >= 16 && mx <= 144 {
			procShowWindow.Call(hwnd, 0)
			restoreMainWindow(appWindow)
			return 0
		}
		// "Çıkış" clicked (Y: 168..222, X: 156..284)
		if my >= 168 && my <= 222 && mx >= 156 && mx <= 284 {
			procShowWindow.Call(hwnd, 0)
			exitRequested.Store(true)
			procPostMessage.Call(appWindow, wmClose, 0, 0)
			return 0
		}
		return 0

	case 0x000F: // WM_PAINT
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		memDC, _, _ := procCreateCompatibleDC.Call(hdc)
		memBmp, _, _ := procCreateCompatibleBmp.Call(hdc, flyoutWidth, flyoutHeight)
		oldBmp, _, _ := procSelectObject.Call(memDC, memBmp)

		// ── Theme colors ──
		bgColor := trayBackgroundColor.Load()
		if bgColor == 0 {
			bgColor = 0x001B1111 // fallback dark
		}
		borderColor := trayBorderColor.Load()
		if borderColor == 0 {
			borderColor = 0x00422B2B
		}
		primary := trayPrimaryColor.Load()
		if primary == 0 {
			primary = 0x00F65C8B
		}
		accent := trayAccentColor.Load()
		if accent == 0 {
			accent = primary
		}

		// Derived colors
		surfaceColor := lightenColor(bgColor, 0.06)
		trackColor := lightenColor(bgColor, 0.10)
		cardColor := lightenColor(bgColor, 0.08)
		cardHoverColor := lightenColor(bgColor, 0.15)
		subtleTextColor := lightenColor(bgColor, 0.45)
		dimTextColor := lightenColor(bgColor, 0.60)
		brightTextColor := uint32(0x00FFFFFF)
		separatorColor := lightenColor(bgColor, 0.12)

		// ── Fonts ──
		fontTitle := createFlyoutFont(-11, 600)   // semibold small
		fontPill := createFlyoutFont(-13, 600)     // semibold medium
		fontButton := createFlyoutFont(-13, 500)   // medium
		fontStatus := createFlyoutFont(-11, 400)   // regular small
		defer func() {
			procDeleteObject.Call(fontTitle)
			procDeleteObject.Call(fontPill)
			procDeleteObject.Call(fontButton)
			procDeleteObject.Call(fontStatus)
		}()

		// ══════════════════════════════════════════════════════════════════
		// 1. BACKGROUND FILL
		// ══════════════════════════════════════════════════════════════════
		bgBrush, _, _ := procCreateSolidBrush.Call(uintptr(surfaceColor))
		bgRect := rectangle{0, 0, flyoutWidth, flyoutHeight}
		procFillRect.Call(memDC, uintptr(unsafe.Pointer(&bgRect)), bgBrush)
		procDeleteObject.Call(bgBrush)

		// ── Top accent glow line (2px gradient-like line) ──
		glowBrush, _, _ := procCreateSolidBrush.Call(uintptr(primary))
		glowRect := rectangle{0, 0, flyoutWidth, 2}
		procFillRect.Call(memDC, uintptr(unsafe.Pointer(&glowRect)), glowBrush)
		procDeleteObject.Call(glowBrush)

		// ── Outer border ──
		borderPen, _, _ := procCreatePen.Call(0, 1, uintptr(borderColor))
		transBrush, _, _ := procCreateSolidBrush.Call(uintptr(surfaceColor))
		procSelectObject.Call(memDC, borderPen)
		procSelectObject.Call(memDC, transBrush)
		procRoundRect.Call(memDC, 0, 0, flyoutWidth, flyoutHeight, 20, 20)
		procDeleteObject.Call(borderPen)
		procDeleteObject.Call(transBrush)

		procSetBkMode.Call(memDC, 1) // TRANSPARENT

		// ══════════════════════════════════════════════════════════════════
		// 2. HEADER: "NEXDNS" brand + active dot
		// ══════════════════════════════════════════════════════════════════
		procSelectObject.Call(memDC, fontTitle)

		brandStr, _ := windows.UTF16PtrFromString("⚡ NEXDNS")
		brandRect := rectangle{16, 8, 180, 28}
		procSetTextColor.Call(memDC, uintptr(primary))
		procDrawText.Call(memDC, uintptr(unsafe.Pointer(brandStr)), ^uintptr(0), uintptr(unsafe.Pointer(&brandRect)), 0x0020|0x0004)

		statusDotStr, _ := windows.UTF16PtrFromString("● Çevrimiçi")
		statusDotRect := rectangle{180, 8, 284, 28}
		procSetTextColor.Call(memDC, 0x0050D07A) // vibrant green
		procDrawText.Call(memDC, uintptr(unsafe.Pointer(statusDotStr)), ^uintptr(0), uintptr(unsafe.Pointer(&statusDotRect)), 0x0020|0x0004|0x0002)

		// ══════════════════════════════════════════════════════════════════
		// 2.1 PING / GECİKME GÖSTERGE KARTI (Latency Badge Card)
		// ══════════════════════════════════════════════════════════════════
		badgeBrush, _, _ := procCreateSolidBrush.Call(uintptr(cardColor))
		badgePen, _, _ := procCreatePen.Call(0, 1, uintptr(borderColor))
		procSelectObject.Call(memDC, badgeBrush)
		procSelectObject.Call(memDC, badgePen)
		procRoundRect.Call(memDC, 16, 34, 284, 68, 12, 12)
		procDeleteObject.Call(badgeBrush)
		procDeleteObject.Call(badgePen)

		// Ping Label
		procSelectObject.Call(memDC, fontStatus)
		pingLblStr, _ := windows.UTF16PtrFromString("📶  DNS Gecikmesi:")
		pingLblRect := rectangle{26, 34, 180, 68}
		procSetTextColor.Call(memDC, uintptr(dimTextColor))
		procDrawText.Call(memDC, uintptr(unsafe.Pointer(pingLblStr)), ^uintptr(0), uintptr(unsafe.Pointer(&pingLblRect)), 0x0020|0x0004)

		// Ping Value & Color Badge
		lat := trayDNSLatencyMs.Load()
		latText := "Ölçülüyor..."
		latColor := uint32(0x00B8A394) // dim gray
		if lat >= 0 {
			latText = fmt.Sprintf("%d ms", lat)
			if lat <= 35 {
				latColor = 0x0050D07A // green (fast)
			} else if lat <= 80 {
				latColor = 0x0038BDF8 // light blue
			} else {
				latColor = 0x007171F8 // orange/soft red
			}
		}
		procSelectObject.Call(memDC, fontPill)
		latValStr, _ := windows.UTF16PtrFromString(latText)
		latValRect := rectangle{170, 34, 274, 68}
		procSetTextColor.Call(memDC, uintptr(latColor))
		procDrawText.Call(memDC, uintptr(unsafe.Pointer(latValStr)), ^uintptr(0), uintptr(unsafe.Pointer(&latValRect)), 0x0020|0x0004|0x0002)

		// ══════════════════════════════════════════════════════════════════
		// 3. MODE SWITCH (Pill/Segmented Control) (Y: 76..120)
		// ══════════════════════════════════════════════════════════════════

		// Track background (X: 16..284, Y: 76..120)
		trackBrush, _, _ := procCreateSolidBrush.Call(uintptr(trackColor))
		trackPen, _, _ := procCreatePen.Call(0, 1, uintptr(borderColor))
		procSelectObject.Call(memDC, trackBrush)
		procSelectObject.Call(memDC, trackPen)
		procRoundRect.Call(memDC, 16, 76, 284, 120, 16, 16)
		procDeleteObject.Call(trackBrush)
		procDeleteObject.Call(trackPen)

		// Animated pill thumb
		pillWidth := int32(131)
		thumbX := int32(19) + int32(pillAnimPos*float64(284-19-pillWidth-3))
		thumbBrush, _, _ := procCreateSolidBrush.Call(uintptr(primary))
		thumbPen, _, _ := procCreatePen.Call(0, 1, uintptr(primary))
		procSelectObject.Call(memDC, thumbBrush)
		procSelectObject.Call(memDC, thumbPen)
		procRoundRect.Call(memDC, uintptr(thumbX), 79, uintptr(thumbX+pillWidth), 117, 14, 14)
		procDeleteObject.Call(thumbBrush)
		procDeleteObject.Call(thumbPen)

		// Inner glow highlight on thumb
		thumbGlow := lightenColor(uint32(primary), 0.25)
		glowPen, _, _ := procCreatePen.Call(0, 1, uintptr(thumbGlow))
		innerBrush, _, _ := procCreateSolidBrush.Call(uintptr(primary))
		procSelectObject.Call(memDC, glowPen)
		procSelectObject.Call(memDC, innerBrush)
		procRoundRect.Call(memDC, uintptr(thumbX+1), 80, uintptr(thumbX+pillWidth-1), 84, 4, 4)
		procDeleteObject.Call(glowPen)
		procDeleteObject.Call(innerBrush)

		// Pill labels
		procSelectObject.Call(memDC, fontPill)
		lblLeft, _ := windows.UTF16PtrFromString("Tüm Sistem")
		lblRight, _ := windows.UTF16PtrFromString("Seçili Uygulama")
		rcLeft := rectangle{16, 76, 150, 120}
		rcRight := rectangle{150, 76, 284, 120}

		colLeft := subtleTextColor
		colRight := subtleTextColor
		if pillAnimPos < 0.5 {
			colLeft = brightTextColor
		} else {
			colRight = brightTextColor
		}
		if flyoutHover == 3 && pillAnimPos >= 0.5 {
			colLeft = dimTextColor
		}
		if flyoutHover == 4 && pillAnimPos < 0.5 {
			colRight = dimTextColor
		}

		procSetTextColor.Call(memDC, uintptr(colLeft))
		procDrawText.Call(memDC, uintptr(unsafe.Pointer(lblLeft)), ^uintptr(0), uintptr(unsafe.Pointer(&rcLeft)), 0x0020|0x0004|0x0001)
		procSetTextColor.Call(memDC, uintptr(colRight))
		procDrawText.Call(memDC, uintptr(unsafe.Pointer(lblRight)), ^uintptr(0), uintptr(unsafe.Pointer(&rcRight)), 0x0020|0x0004|0x0001)

		// ══════════════════════════════════════════════════════════════════
		// 4. MODE STATUS TEXT
		// ══════════════════════════════════════════════════════════════════
		procSelectObject.Call(memDC, fontStatus)
		var modeDesc *uint16
		if pillAnimPos < 0.5 {
			modeDesc, _ = windows.UTF16PtrFromString("Tüm sistem trafiği NEXDNS ile korunuyor")
		} else {
			modeDesc, _ = windows.UTF16PtrFromString("Yalnızca seçili uygulamalar yönlendiriliyor")
		}
		modeRect := rectangle{16, 126, 284, 146}
		procSetTextColor.Call(memDC, uintptr(subtleTextColor))
		procDrawText.Call(memDC, uintptr(unsafe.Pointer(modeDesc)), ^uintptr(0), uintptr(unsafe.Pointer(&modeRect)), 0x0020|0x0004|0x0001)

		// ── Separator line above buttons ──
		sep2Brush, _, _ := procCreateSolidBrush.Call(uintptr(separatorColor))
		sep2Rect := rectangle{16, 154, 284, 155}
		procFillRect.Call(memDC, uintptr(unsafe.Pointer(&sep2Rect)), sep2Brush)
		procDeleteObject.Call(sep2Brush)

		// ══════════════════════════════════════════════════════════════════
		// 5. ACTION BUTTONS (Split 50-50, Drawing layout) (Y: 164..222)
		// ══════════════════════════════════════════════════════════════════
		procSelectObject.Call(memDC, fontButton)

		// Button 1 (Left 50%): "NEXDNS'e Dön" (X: 16..144, Y: 164..222)
		b1Bg := cardColor
		b1Text := dimTextColor
		b1Border := borderColor
		if flyoutHover == 1 {
			b1Bg = uint32(primary)
			b1Text = brightTextColor
			b1Border = uint32(primary)
		}
		b1Brush, _, _ := procCreateSolidBrush.Call(uintptr(b1Bg))
		b1Pen, _, _ := procCreatePen.Call(0, 1, uintptr(b1Border))
		procSelectObject.Call(memDC, b1Brush)
		procSelectObject.Call(memDC, b1Pen)
		procRoundRect.Call(memDC, 16, 164, 144, 222, 14, 14)
		procDeleteObject.Call(b1Brush)
		procDeleteObject.Call(b1Pen)

		b1Str, _ := windows.UTF16PtrFromString("🖥 NEXDNS'e Dön")
		rcB1 := rectangle{16, 164, 144, 222}
		procSetTextColor.Call(memDC, uintptr(b1Text))
		procDrawText.Call(memDC, uintptr(unsafe.Pointer(b1Str)), ^uintptr(0), uintptr(unsafe.Pointer(&rcB1)), 0x0020|0x0004|0x0001)

		// Button 2 (Right 50%): "Çıkış" (X: 156..284, Y: 164..222)
		b2Bg := cardColor
		b2Text := uint32(0x007171F8) // soft red
		b2Border := borderColor
		if flyoutHover == 2 {
			b2Bg = cardHoverColor
			b2Text = uint32(0x005555FF) // bright red
			b2Border = lightenColor(borderColor, 0.20)
		}
		b2Brush, _, _ := procCreateSolidBrush.Call(uintptr(b2Bg))
		b2Pen, _, _ := procCreatePen.Call(0, 1, uintptr(b2Border))
		procSelectObject.Call(memDC, b2Brush)
		procSelectObject.Call(memDC, b2Pen)
		procRoundRect.Call(memDC, 156, 164, 284, 222, 14, 14)
		procDeleteObject.Call(b2Brush)
		procDeleteObject.Call(b2Pen)

		b2Str, _ := windows.UTF16PtrFromString("⏻ Çıkış")
		rcB2 := rectangle{156, 164, 284, 222}
		procSetTextColor.Call(memDC, uintptr(b2Text))
		procDrawText.Call(memDC, uintptr(unsafe.Pointer(b2Str)), ^uintptr(0), uintptr(unsafe.Pointer(&rcB2)), 0x0020|0x0004|0x0001)

		// ══════════════════════════════════════════════════════════════════
		// 6. BLIT TO SCREEN
		// ══════════════════════════════════════════════════════════════════
		procBitBlt.Call(hdc, 0, 0, flyoutWidth, flyoutHeight, memDC, 0, 0, 0x00CC0020)
		procSelectObject.Call(memDC, oldBmp)
		procDeleteObject.Call(memBmp)
		procDeleteDC.Call(memDC)
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

		runtime.KeepAlive(brandStr)
		runtime.KeepAlive(statusDotStr)
		runtime.KeepAlive(pingLblStr)
		runtime.KeepAlive(latValStr)
		runtime.KeepAlive(modeDesc)
		runtime.KeepAlive(lblLeft)
		runtime.KeepAlive(lblRight)
		runtime.KeepAlive(b1Str)
		runtime.KeepAlive(b2Str)
		return 0
	}
	res, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return res
}

func restoreMainWindow(hwnd uintptr) {
	showWindow(hwnd, swRestore)
	_, _, _ = procSetForeground.Call(hwnd)
}

func showWindow(hwnd uintptr, command int32) {
	_, _, _ = procShowWindow.Call(hwnd, uintptr(command))
}

func updateWindowIcon(hwnd uintptr, primary, accent string) error {
	setTrayTheme("", "", primary, accent)
	if currentSmallIcon == 0 || currentLargeIcon == 0 {
		return installBrandIcons(hwnd)
	}
	if hwnd != 0 {
		_, _, _ = procSendMessage.Call(hwnd, wmSetIcon, iconSmall, uintptr(currentSmallIcon))
		_, _, _ = procSendMessage.Call(hwnd, wmSetIcon, iconBig, uintptr(currentLargeIcon))
	}
	if trayInstalled {
		trayData.hIcon = currentSmallIcon
		_, _, _ = procShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&trayData)))
	}
	return nil
}

func destroyCurrentWindowIcon() {
	if currentSmallIcon != 0 {
		_, _, _ = procDestroyIcon.Call(uintptr(currentSmallIcon))
		currentSmallIcon = 0
	}
	if currentLargeIcon != 0 {
		_, _, _ = procDestroyIcon.Call(uintptr(currentLargeIcon))
		currentLargeIcon = 0
	}
}

func createBrandIcon(primaryHex, accentHex string) (windows.Handle, error) {
	primary, err := parseHexColor(primaryHex)
	if err != nil {
		return 0, err
	}
	accent, err := parseHexColor(accentHex)
	if err != nil {
		return 0, err
	}

	const outputSize = 32
	const supersample = 4
	const highSize = outputSize * supersample
	high := image.NewRGBA(image.Rect(0, 0, highSize, highSize))
	brandPath := [][2]float64{{5, 19}, {5, 5}, {8, 5}, {16, 14}, {16, 5}, {19, 5}, {19, 19}, {16, 19}, {8, 10}, {8, 19}}
	polygon := make([][2]float64, len(brandPath))
	for i, p := range brandPath {
		polygon[i] = [2]float64{p[0] * float64(highSize) / 24, p[1] * float64(highSize) / 24}
	}
	for y := 0; y < highSize; y++ {
		for x := 0; x < highSize; x++ {
			px := (float64(x) + 0.5) / supersample
			py := (float64(y) + 0.5) / supersample
			if insideRoundedRect(px, py, 0.5, 0.5, 31.5, 31.5, 7.2) {
				high.SetRGBA(x, y, color.RGBA{R: 43, G: 45, B: 61, A: 255})
			}
			if insideRoundedRect(px, py, 1.35, 1.35, 30.65, 30.65, 6.0) {
				high.SetRGBA(x, y, color.RGBA{R: 24, G: 25, B: 38, A: 255})
			}
			if pointInPolygon(px, py, polygon) {
				t := (px/32 + 1 - py/32) / 2
				if t < 0 {
					t = 0
				} else if t > 1 {
					t = 1
				}
				high.SetRGBA(x, y, interpolateColor(primary, accent, t))
			}
		}
	}

	iconImage := image.NewRGBA(image.Rect(0, 0, outputSize, outputSize))
	for y := 0; y < outputSize; y++ {
		for x := 0; x < outputSize; x++ {
			var sumR, sumG, sumB, sumA uint32
			for sy := 0; sy < supersample; sy++ {
				for sx := 0; sx < supersample; sx++ {
					r, g, b, a := high.At(x*supersample+sx, y*supersample+sy).RGBA()
					sumR += r >> 8
					sumG += g >> 8
					sumB += b >> 8
					sumA += a >> 8
				}
			}
			iconImage.SetRGBA(x, y, color.RGBA{
				R: uint8(sumR / 16), G: uint8(sumG / 16), B: uint8(sumB / 16), A: uint8(sumA / 16),
			})
		}
	}

	return createNativeIcon(iconImage)
}

func createNativeIcon(iconImage *image.RGBA) (windows.Handle, error) {
	width, height := iconImage.Bounds().Dx(), iconImage.Bounds().Dy()
	var dibBits uintptr
	info := bitmapInfo{header: bitmapInfoHeader{
		size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), width: int32(width), height: -int32(height),
		planes: 1, bitCount: 32, compression: 0, sizeImage: uint32(width * height * 4),
	}}
	colorBitmap, _, callErr := procCreateDIBSection.Call(
		0,
		uintptr(unsafe.Pointer(&info)),
		dibRGBColors,
		uintptr(unsafe.Pointer(&dibBits)),
		0,
		0,
	)
	if colorBitmap == 0 || dibBits == 0 {
		return 0, fmt.Errorf("create NEXDNS color bitmap: %v", callErr)
	}

	maskBits := make([]byte, height*((width+31)/32*4))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if iconImage.RGBAAt(x, y).A < 128 {
				maskBits[y*((width+31)/32*4)+x/8] |= 1 << (7 - uint(x%8))
			}
		}
	}
	maskBitmap, _, callErr := procCreateBitmap.Call(
		uintptr(width), uintptr(height), 1, 1, uintptr(unsafe.Pointer(&maskBits[0])),
	)
	if maskBitmap == 0 {
		_, _, _ = procDeleteObject.Call(colorBitmap)
		return 0, fmt.Errorf("create NEXDNS transparency mask: %v", callErr)
	}

	bitmapPixels := unsafe.Slice((*byte)(unsafe.Pointer(dibBits)), width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := iconImage.RGBAAt(x, y)
			i := (y*width + x) * 4
			bitmapPixels[i+0] = uint8(uint16(c.B) * uint16(c.A) / 255)
			bitmapPixels[i+1] = uint8(uint16(c.G) * uint16(c.A) / 255)
			bitmapPixels[i+2] = uint8(uint16(c.R) * uint16(c.A) / 255)
			bitmapPixels[i+3] = c.A
		}
	}

	iconDetails := iconInfo{icon: 1, mask: windows.Handle(maskBitmap), color: windows.Handle(colorBitmap)}
	icon, _, callErr := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&iconDetails)))
	runtime.KeepAlive(iconImage)
	runtime.KeepAlive(bitmapPixels)
	runtime.KeepAlive(maskBits)
	runtime.KeepAlive(info)
	_, _, _ = procDeleteObject.Call(maskBitmap)
	_, _, _ = procDeleteObject.Call(colorBitmap)
	if icon == 0 {
		return 0, fmt.Errorf("create NEXDNS color icon: %v", callErr)
	}
	return windows.Handle(icon), nil
}

func colorRefFromRGBA(value color.RGBA) uint32 {
	return uint32(value.R) | uint32(value.G)<<8 | uint32(value.B)<<16
}

func parseHexColor(value string) (color.RGBA, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 {
		return color.RGBA{}, fmt.Errorf("invalid icon color %q", value)
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(value, "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{}, fmt.Errorf("invalid icon color %q: %w", value, err)
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}, nil
}

func insideRoundedRect(x, y, left, top, right, bottom, radius float64) bool {
	nearestX := x
	if nearestX < left+radius {
		nearestX = left + radius
	} else if nearestX > right-radius {
		nearestX = right - radius
	}
	nearestY := y
	if nearestY < top+radius {
		nearestY = top + radius
	} else if nearestY > bottom-radius {
		nearestY = bottom - radius
	}
	dx, dy := x-nearestX, y-nearestY
	return x >= left && x <= right && y >= top && y <= bottom && dx*dx+dy*dy <= radius*radius
}

func pointInPolygon(x, y float64, polygon [][2]float64) bool {
	inside := false
	for i, j := 0, len(polygon)-1; i < len(polygon); j, i = i, i+1 {
		xi, yi := polygon[i][0], polygon[i][1]
		xj, yj := polygon[j][0], polygon[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			inside = !inside
		}
	}
	return inside
}

func interpolateColor(from, to color.RGBA, amount float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(from.R) + (float64(to.R)-float64(from.R))*amount),
		G: uint8(float64(from.G) + (float64(to.G)-float64(from.G))*amount),
		B: uint8(float64(from.B) + (float64(to.B)-float64(from.B))*amount),
		A: 255,
	}
}
