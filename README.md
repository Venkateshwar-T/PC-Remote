# PC Remote

PC Remote is a lightweight, decentralized background daemon for Windows that enables low-latency system telemetry monitoring and workstation power management from any mobile device over a secure local or edge-hosted Progressive Web Application (PWA).

[![Release](https://img.shields.io/github/v/release/Venkateshwar-T/PC-Remote?style=flat-square)](https://github.com/Venkateshwar-T/PC-Remote/releases/latest)
[![Go Version](https://img.shields.io/badge/go-1.22+-007d9c?style=flat-square)](https://go.dev/)
[![Platform](https://img.shields.io/badge/platform-Windows%2010%20%7C%2011%20(x64)-0078d4?style=flat-square)](https://microsoft.com/windows)
[![License](https://img.shields.io/badge/license-MIT-333333?style=flat-square)](LICENSE)

---

## Overview

Unlike traditional remote management software, PC Remote operates without third-party cloud accounts, telemetry trackers, or proprietary relays. It pairs directly with mobile devices via a native Win32 QR code and runs as an unprivileged, low-footprint background service.

### Key Capabilities

* **Minimal System Footprint**: Engineered in pure Go using direct Win32 system calls (`kernel32`, `user32`, `powrprof`). Operates at **0.0% idle CPU** and **~10 MB memory usage**.
* **Zero-Host Mobile Client**: Serves a standalone Progressive Web App through Cloudflare Pages edge hosting or direct local HTTP. Installs directly to iOS and Android home screens without requiring app store installation.
* **Real-Time Telemetry**: Queries low-level hardware counters for battery state, AC charging status, CPU utilization, physical RAM allocation, system uptime, and active network interfaces.
* **Workstation Power Controls**: Executes instantaneous workstation lock, system suspend (sleep), reboot, and shutdown commands.
* **Non-Intrusive Background Execution**: Integrates natively into the Windows notification tray (`Shell_NotifyIconW`), with silent startup options on system boot and single-instance mutex enforcement.

---

## Security Model

Security and access control are strictly enforced at the workstation level:

* **Desktop-Exclusive Master PIN**: The 6-digit Master PIN can only be established and updated directly on the physical workstation. Remote modifications via the API are permanently forbidden with `403 Forbidden`.
* **PBKDF2 Key Derivation**: PINs are hashed using PBKDF2-HMAC-SHA256 with 100,000 iterations and a 16-byte cryptographically secure random salt (`crypto/rand`).
* **Timing-Attack Resistance**: Authentication verifies candidate hashes using constant-time byte comparisons (`subtle.ConstantTimeCompare`), preventing side-channel analysis.
* **Anti-Brute-Force Rate Limiting**: Five consecutive failed attempts trigger an automatic three-minute lockout. Lockout states persist across process restarts.
* **Request Validation**: Local HTTP server enforces a 64 KB payload cap (`http.MaxBytesReader`) and restricts path traversal attempts.

---

## Getting Started

### Installation

1. Download **`PC-Remote-Setup.exe`** from the [Latest Release](https://github.com/Venkateshwar-T/PC-Remote/releases/latest).
2. Run the installer. By default, PC Remote installs to `%LOCALAPPDATA%\Programs\PC Remote` and does not require administrative privileges.
3. Check **Start PC Remote automatically when Windows starts** if background availability on boot is desired.

### Initial Configuration & Pairing

1. Launch **PC Remote** from the Start Menu or Desktop shortcut.
2. When prompted, create a 6-digit Master PIN.
3. A pairing dialog will display a QR code.
4. Scan the QR code using your mobile phone camera to open the dashboard.
5. In your mobile browser, tap **Add to Home Screen** to install the dashboard as an offline-capable application.
6. Enter your 6-digit Master PIN to unlock the control interface.

---

## Building from Source

### Prerequisites

* Windows 10 or 11 (64-bit)
* [Go 1.22+](https://go.dev/dl/)
* [Inno Setup 6](https://jrsoftware.org/isinfo.php) (required only for building the installer)

### Compilation

Clone the repository:
```cmd
git clone https://github.com/Venkateshwar-T/PC-Remote.git
cd PC-Remote
```

Build the standalone GUI binary:
```cmd
scripts\build.bat
```
This produces `PC-Remote.exe` (stripped, native Windows GUI subsystem binary with embedded high-DPI application icons).

Build the complete setup installer:
```cmd
scripts\build_installer.bat
```
This produces `dist\PC-Remote-Setup.exe`.

---

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
