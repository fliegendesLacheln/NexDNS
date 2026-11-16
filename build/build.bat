@echo off
chcp 65001 >nul
echo ========================================================
echo   NEXDNS — ADVANCED NETWORK & DNS MANAGER DERLEME
echo ========================================================
echo.
echo Derleme başlatılıyor...

go build -ldflags "-H windowsgui -s -w" -o NEXDNS.exe ./cmd/nexdns

if %ERRORLEVEL% EQU 0 (
    echo.
    echo [OK] DERLEME BAŞARILI!
    echo [OK] NEXDNS.exe başarıyla oluşturuldu.
    echo.
) else (
    echo.
    echo [ERROR] Derleme sırasında bir hata oluştu!
    echo.
)
pause
