@echo off
setlocal
cd /d "%~dp0\.."

if not exist "PC-Remote.exe" (
    call scripts\build.bat
)

if exist "PC-Remote.exe" (
    start "" "PC-Remote.exe"
)
