# PC Remote 🖥️📱

> Modern, ultra-lightweight, decentralized remote management daemon for Windows with an offline mobile PWA dashboard.

![Windows](https://img.shields.io/badge/Windows-10%20%7C%2011-0078d4?style=flat-square&logo=windows)
![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?style=flat-square&logo=go)
![Architecture](https://img.shields.io/badge/Architecture-Decentralized%20Peer--to--Peer-22c55e?style=flat-square)
![License](https://img.shields.io/badge/License-MIT-gray?style=flat-square)

---

## ✨ Features

- **⚡ Native Windows Daemon**: Built in pure Go with direct Win32 syscalls (`kernel32`, `user32`, `powrprof`). Uses ~15 MB RAM and **0.0% idle CPU**.
- **🔒 Desktop-Secured Master PIN**: 6-digit PIN hashed with 100,000 rounds of PBKDF2-HMAC-SHA256. Constant-time verification (`subtle.ConstantTimeCompare`) prevents timing attacks.
- **🛡️ Anti-Brute-Force**: 5 consecutive failed attempts trigger an automatic 3-minute lockout with live countdown on mobile.
- **📱 Zero-Host Mobile PWA**: Instant pairing via native Win32 QR code dialog. Mobile interface installs to your home screen as a standalone offline PWA.
- **🔋 Live Telemetry**: Real-time battery status & percentage, CPU load, RAM usage (GB & %), system uptime, and Wi-Fi connection info.
- **⚡ Workstation Controls**: Instant Lock Screen, Sleep Mode, System Restart, and Power Off.
- **📦 Clean Installer**: Ships with an automated Inno Setup Windows installer (`PC-Remote-Setup.exe`) with desktop shortcuts, startup option, and clean uninstaller.

---

## 🚀 Quick Start for Users

1. Download **`PC-Remote-Setup.exe`** from [Releases](https://github.com/Venkateshwar-T/PC-Remote/releases/latest).
2. Run the installer and launch **PC Remote**.
3. Create your 6-digit Master PIN on the desktop setup screen.
4. Scan the QR code with your phone camera while connected to the same Wi-Fi.
5. Tap **"Add to Home Screen"** on your phone to install it like a native mobile app!

---

## 🛠️ Building from Source

### Prerequisites
- Windows 10 or 11 (x64)
- [Go 1.22+](https://go.dev/dl/)
- [Inno Setup 6](https://jrsoftware.org/isinfo.php) (optional, for compiling the installer)

### Build the Executable
```cmd
scripts\build.bat
```
Produces `PC-Remote.exe` (stripped, native GUI binary without console window).

### Build the Windows Installer
```cmd
scripts\build_installer.bat
```
Produces `dist\PC-Remote-Setup.exe` with full desktop integration and uninstaller.

---

## 📄 License
MIT License. Free and open source.
