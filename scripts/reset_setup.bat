@echo off
setlocal
cd /d "%~dp0\.."

REM Terminate any running instance first
taskkill /IM PC-Remote.exe /F >nul 2>&1
taskkill /IM LaptopControl.exe /F >nul 2>&1

REM Launch with -reset to trigger First-Time Setup Wizard
start "" "PC-Remote.exe" -reset
