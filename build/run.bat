@echo off
chcp 65001 >nul
echo ========================================================
echo   NEXDNS — ADVANCED NETWORK & DNS MANAGER BAŞLATILIYOR
echo ========================================================
echo.

if exist "..\NEXDNS.exe" (
    start "" "..\NEXDNS.exe"
) else if exist "NEXDNS.exe" (
    start "" "NEXDNS.exe"
) else (
    echo NEXDNS.exe bulunamadı, derleniyor...
    go build -ldflags "-H windowsgui -s -w" -o NEXDNS.exe ./cmd/nexdns
    start "" "NEXDNS.exe"
)
