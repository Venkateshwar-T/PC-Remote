@echo off
setlocal
cd /d "%~dp0\.."

REM 1. Ensure fresh PC-Remote.exe is compiled
call scripts\build.bat
if %ERRORLEVEL% neq 0 exit /b 1

REM 2. Ensure icon.ico exists
if not exist "icon.ico" (
    powershell -NoProfile -Command "Add-Type -AssemblyName System.Drawing; [System.Drawing.Bitmap]$b = [System.Drawing.Bitmap]::FromFile('icon.png'); [System.Drawing.Icon]$i = [System.Drawing.Icon]::FromHandle($b.GetHicon()); $s = [System.IO.File]::Create('icon.ico'); $i.Save($s); $s.Close()"
)

REM 3. Locate ISCC.exe (Inno Setup Compiler)
set "ISCC="
if exist "%LOCALAPPDATA%\Programs\Inno Setup 6\ISCC.exe" (
    set "ISCC=%LOCALAPPDATA%\Programs\Inno Setup 6\ISCC.exe"
) else if exist "C:\Program Files (x86)\Inno Setup 6\ISCC.exe" (
    set "ISCC=C:\Program Files (x86)\Inno Setup 6\ISCC.exe"
) else if exist "C:\Program Files\Inno Setup 6\ISCC.exe" (
    set "ISCC=C:\Program Files\Inno Setup 6\ISCC.exe"
)

if "%ISCC%"=="" (
    where iscc >nul 2>&1
    if %ERRORLEVEL% equ 0 (
        set "ISCC=iscc"
    ) else (
        echo [ERROR] Inno Setup compiler ^(ISCC.exe^) not found.
        echo Please ensure Inno Setup 6 is installed.
        exit /b 1
    )
)

echo [Installer] Building PC-Remote-Setup.exe with Inno Setup...
if not exist "dist" mkdir "dist"
"%ISCC%" installer\setup.iss

if %ERRORLEVEL% equ 0 (
    echo.
    echo ========================================================
    echo   [SUCCESS] Installer successfully created!
    echo   File: dist\PC-Remote-Setup.exe
    echo ========================================================
    dir dist\PC-Remote-Setup.exe | findstr PC-Remote-Setup.exe
) else (
    echo [ERROR] Inno Setup compilation failed.
    exit /b 1
)
