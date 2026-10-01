# PC Remote

PC Remote is an open-source, end-to-end encrypted (E2EE) remote control and hardware telemetry daemon for Windows. It allows controlling your PC from anywhere in the world—whether over your local home Wi-Fi or on mobile cellular data (5G/4G)—using a zero-install Progressive Web App (PWA).

[![Go Version](https://img.shields.io/badge/go-1.22+-007d9c?style=flat-square)](https://go.dev/)
[![Platform](https://img.shields.io/badge/platform-Windows%2010%20%7C%2011%20(x64)-0078d4?style=flat-square)](https://microsoft.com/windows)
[![Protocol](https://img.shields.io/badge/protocol-Nostr%20E2EE%20(NIP--44%20v2)-purple?style=flat-square)](https://github.com/nostr-protocol/nips/blob/master/44.md)
[![License](https://img.shields.io/badge/license-MIT-333333?style=flat-square)](LICENSE)

---

## Architecture Overview

PC Remote employs a unified cryptographic architecture: regardless of transport (Direct LAN HTTP or public Nostr relays), every command travels inside the exact same cryptographically signed and NIP-44 encrypted envelope.

```
                              PHONE CLIENT (PWA)
                                      |
                           Local Master PIN Unlock
                        (Web Crypto PBKDF2 / AES-GCM)
                                      |
                            phonePrivkey (secp256k1)
                                      |
                         BIP-340 Sign + NIP-44 Encrypt
                                      |
                      +---------------+---------------+
                      |                               |
                 Direct LAN                      Nostr Relays
            POST /api/control               (damus.io, nos.lol,
        (Cryptographic Envelope)             relay.primal.net)
                      |                               |
                      +---------------+---------------+
                                      |
                                      v
                              PC REMOTE DAEMON
                                      |
                         1. Verify BIP-340 Signature
                         2. NIP-44 Decrypt with LaptopPrivKey
                         3. Server-side Authorization Check
                         4. Scoped Replay & Freshness Guard
                         5. Strict Action Allowlist & Bounds Check
                         6. Native Win32 Execution (Lock, Sleep, Reboot)
```

---

## Cryptographic Security Model

### 1. Unified Cryptographic Envelope
* **Transport Independence**: Whether connecting over local Wi-Fi or the public internet, the command envelope format is identical: a signed Nostr Event (Kind 4) with NIP-44 v2 ciphertext addressed to the laptop's public key (`p` tag).
* **Zero Plaintext PINs**: The Master PIN is never sent as a plaintext API field over LAN or relay. Remote control commands authenticate using the phone's cryptographic identity (`phonePrivkey` BIP-340 Schnorr signature).
* **NIP-44 v2 Encryption Only**: Legacy NIP-04 (AES-CBC without padding) has been completely removed. PC Remote strictly uses NIP-44 v2 (ChaCha20-Poly1305 with HKDF-SHA256 and deterministic padding).

### 2. Master PIN Separation & Browser Vault
* **Remote Authorization (Laptop)**: During initial pairing, the user enters the 6-digit Master PIN. The laptop verifies the PIN using PBKDF2-HMAC-SHA256 (100,000 iterations), validates the transient pairing token, and authorizes the phone's public key. After pairing, the PIN is never transmitted again.
* **Local Phone Unlock (Browser)**: When already paired, the phone requires the Master PIN to unlock the local dashboard. The PIN derives an AES-GCM-256 key via Web Crypto PBKDF2 to decrypt `phonePrivkey` stored in IndexedDB.
* **Integrity Tag Verification**: Arbitrary 6-digit guesses fail the AES-GCM 128-bit authentication tag check and are rejected locally. The Master PIN is never sent to the PC to unlock the UI.
* **Volatile Memory Only**: The decrypted private key exists in browser memory only while the session is unlocked.

### 3. Windows DPAPI Config Protection
* **Protected Key at Rest**: The long-term laptop private key is encrypted on disk using Windows Data Protection API (`CryptProtectData`), tied to the user's Windows login session. Plaintext private keys are never stored in `config.json` and are never printed to logs.
* **Automatic Migration**: Legacy plaintext configurations are migrated to DPAPI automatically on first startup.

### 4. Single-Use Pairing Tokens & Strict Expiry
* **128-bit Entropy**: Pairing tokens are generated using `crypto/rand` (16 bytes = 32 hex characters).
* **Enforced 5-Minute Window**: Every pairing validation strictly enforces `time.Now().Before(expiresAt)`.
* **Atomic Consumption & Race Prevention**: Protected by mutex. Successful pairing consumes the token immediately, regenerates a fresh token for future pairings, and rejects any reuse or concurrent race attempts.

### 5. Hardened ReplayGuard
* **Sender-Scoped**: Cache keys are scoped to `(senderPubKey, requestID)`, preventing cross-client collision attacks.
* **Timestamp Cross-Verification**: Both the Nostr `event.CreatedAt` and the inner payload timestamp are checked. Events older than 120 seconds, more than 30 seconds in the future, or differing from the inner timestamp by more than 30 seconds are dropped.
* **Strictly Bounded Memory**: The replay cache enforces a hard capacity limit with deterministic pruning and eviction of oldest entries, neutralizing memory exhaustion DoS attacks.

---

## What the Relay Sees vs. What the Relay Cannot See

| Data Attribute | Visible to Relay? | Explanation |
| :--- | :---: | :--- |
| **Master PIN** | **NO** | Never transmitted with remote commands. Only present inside ciphertext during initial pairing handshake. |
| **Command Actions (`lock`, `sleep`, `restart`, `shutdown`)** | **NO** | Sealed inside NIP-44 AEAD ciphertext. |
| **System Telemetry (CPU, RAM, Battery, Uptime)** | **NO** | Encrypted response payload only readable with phone's private key. |
| **Workstation Identity / Domain** | **NO** | Devices are identified on the relay network solely by 32-byte public key hashes. |
| **Private Keys** | **NO** | Private keys never leave their respective hosts. |
| **Network Metadata** | **YES** | Public relays observe the public key of the sender, the recipient tag (`#p`), event timestamps, and ciphertext size. |

---

## Known Security Boundaries & Limitations

* **Browser Storage Sandboxing**: While `phonePrivkey` is encrypted at rest using AES-GCM derived from your Master PIN, browser storage (IndexedDB/localStorage) is subject to the security of the host device. Malicious browser extensions or a compromised mobile OS with root access could inspect application memory.
* **Relay Metadata**: Public relays can observe traffic timing and the volume of events exchanged between public key hashes.
* **Windows Standby**: After entering `sleep` or `shutdown`, the PC network interface powers down; waking the PC requires physical power button access or Wake-on-LAN.

---

## Getting Started

### Installation via Setup Installer
1. Download **`PC-Remote-Setup.exe`** from [Releases](https://github.com/Venkateshwar-T/PC-Remote/releases).
2. Run the installer. PC Remote installs to `%LOCALAPPDATA%\Programs\PC Remote` (no administrator privileges required).
3. Optionally check **Start PC Remote automatically when Windows starts**.

### Pairing Your Phone
1. Launch **PC Remote** from the Start Menu or Desktop.
2. If first launch, set your 6-digit Master PIN on the desktop prompt.
3. A pairing window will appear with a high-resolution QR code.
4. Scan the QR code using your phone camera to open the Progressive Web App.
5. In your mobile browser, select **Add to Home Screen** for full-screen application experience.
6. Enter your 6-digit Master PIN once to complete the cryptographic pairing handshake.
7. Your phone is now paired and can control the PC from anywhere in the world.

---

## Building from Source

### Prerequisites
* Windows 10 or 11 (64-bit)
* [Go 1.22+](https://go.dev/dl/)
* [Node.js 18+](https://nodejs.org/) (for automated state machine testing)
* [Inno Setup 6](https://jrsoftware.org/isinfo.php) (only needed for packaging the installer)

### Compilation & Tests
```cmd
git clone https://github.com/Venkateshwar-T/PC-Remote.git
cd PC-Remote

REM Run all Go unit and protocol tests
go test -v ./...

REM Run PWA state machine & Web Crypto security tests
node tests/state_test.js

REM Build standalone stripped Windows GUI binary (PC-Remote.exe)
scripts\build.bat

REM Build complete installer (dist\PC-Remote-Setup.exe)
scripts\build_installer.bat
```

---

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
