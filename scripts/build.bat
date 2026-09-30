@echo off
setlocal
cd /d "%~dp0\.."

REM Ensure Go is in PATH if installed in standard location
if exist "C:\Program Files\Go\bin\go.exe" (
    set "PATH=C:\Program Files\Go\bin;%PATH%"
)

where go >nul 2>&1
if %ERRORLEVEL% neq 0 (
    echo [ERROR] Go compiler not found in PATH or C:\Program Files\Go\bin.
    echo Please ensure Go installation is complete.
    exit /b 1
)

echo [1/3] Downloading dependencies...
go get github.com/gorilla/websocket
go get github.com/skip2/go-qrcode
go mod tidy

REM Generate Windows PE icon resources if icon.png exists
if exist "icon.png" (
    if exist "%USERPROFILE%\go\bin\go-winres.exe" (
        "%USERPROFILE%\go\bin\go-winres.exe" simply --icon icon.png --manifest gui --product-name "PC Remote" --file-description "PC Remote Decentralized Daemon" --out cmd/laptopcontrol/rsrc >nul 2>&1
    )
)

echo [2/3] Compiling PC-Remote.exe (pure native GUI, stripped binary, no terminal)...
if exist "PC-Remote.exe~" del /f /q "PC-Remote.exe~" >nul 2>&1
go build -ldflags="-s -w -H=windowsgui" -o PC-Remote.exe ./cmd/laptopcontrol
if exist "PC-Remote.exe~" del /f /q "PC-Remote.exe~" >nul 2>&1

if %ERRORLEVEL% equ 0 (
    echo [3/3] Build succeeded: PC-Remote.exe - GUI mode with embedded modern icon
    dir PC-Remote.exe | findstr PC-Remote.exe
) else (
    echo [ERROR] Build failed.
    exit /b 1
)
