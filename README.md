# PC Remote

PC Remote is an open-source, end-to-end encrypted (E2EE) remote control and hardware telemetry daemon for Windows. It allows controlling your PC from anywhere in the world—whether over your local home Wi-Fi or on mobile cellular data (5G/4G)—using a zero-install Progressive Web App (PWA).

[![Go Version](https://img.shields.io/badge/go-1.22+-007d9c?style=flat-square)](https://go.dev/)
[![Platform](https://img.shields.io/badge/platform-Windows%2010%20%7C%2011%20(x64)-0078d4?style=flat-square)](https://microsoft.com/windows)
[![Protocol](https://img.shields.io/badge/protocol-Nostr%20E2EE%20(NIP--44)-purple?style=flat-square)](https://github.com/nostr-protocol/nips/blob/master/44.md)
[![License](https://img.shields.io/badge/license-MIT-333333?style=flat-square)](LICENSE)

---

## Architecture Overview

PC Remote employs a hybrid dual-path communication architecture designed to operate seamlessly across both private local networks and the public internet without port forwarding, dynamic DNS, or centralized account management.

```
PHONE CLIENT (PWA)                           PUBLIC NOSTR RELAY MESH                     PC REMOTE DAEMON
===================                           =======================                     ================
1. Direct LAN Probe  ---- [HTTP Fast-Path (Same Wi-Fi)] --------------------------------> Local Server (:8765)
                                                                                          
2. Remote Internet   ---- [NIP-44 Encrypted Nostr Event] ---> (relay.damus.io)  -------> Outbound Subscription
     (5G / LTE)           Signed with BIP-340 Schnorr         (nos.lol)                   Verifies signature,
                          Addressed to #p: laptopPubKey       (relay.primal.net)          checks replay guard,
                                                                                          decrypts payload,
                          <--- [NIP-44 Encrypted Response] <-- (Broadcast Result) <------ executes Win32 action
```

### 1. Direct LAN Mode (Local Wi-Fi)
When both your phone and PC are connected to the same local network and accessed over direct HTTP, commands are routed directly via local HTTP POST requests with sub-10ms response times.

### 2. Remote Internet Mode (Cellular 5G / Remote Network)
When outside home Wi-Fi or when mobile browsers enforce Local Network Access / Mixed Content restrictions, PC Remote communicates over the decentralized Nostr relay network:
* The PC daemon maintains persistent outbound WebSocket connections to multiple independent public Nostr relays (`relay.damus.io`, `nos.lol`, `relay.primal.net`).
* No incoming ports or router configuration required (NAT-traversal is automatic via outbound connections).
* The phone publishes signed, end-to-end encrypted commands targeted to the PC's public key.
* The PC processes the action and publishes an encrypted reply back to the phone.

---

## Cryptographic Security & Privacy Model

### True End-to-End Encryption (NIP-44 / NIP-04)
* **Cryptographic Identity**: Devices use genuine secp256k1 keypairs conforming to BIP-340. Public keys are derived mathematically from private keys.
* **Ciphertext Guarantees**: All command payloads and telemetry responses are encrypted using **NIP-44 v2** (ChaCha20-Poly1305 with HKDF shared keys and payload padding).
* **Cryptographic Signatures**: Every transmitted event is authenticated with a BIP-340 Schnorr signature. Tampered payloads or unauthorized senders are dropped immediately.

### What the Relay Can See vs. Cannot See

| Data Attribute | Visible to Relay? | Explanation |
| :--- | :---: | :--- |
| **Master PIN** | **NO** | The Master PIN is never transmitted over the relay network after initial pairing. |
| **Control Actions (`lock`, `shutdown`)** | **NO** | Sealed inside NIP-44 AEAD ciphertext. |
| **System Telemetry (CPU, RAM, Battery)** | **NO** | Encrypted response payload only readable with phone's private key. |
| **Workstation Identity / Domain** | **NO** | No personal names or domain names are exposed; devices are identified only by 32-byte public key hashes. |
| **Routing Metadata** | **YES** | The public relay sees event metadata (timestamp, sender public key, and recipient tag `#p`). |

### Replay & Anti-Tamper Protection
* **Sliding-Window Replay Guard**: Every command contains a cryptographically unique request ID (`id`). The PC maintains a thread-safe sliding-window cache; duplicate request IDs are rejected immediately.
* **Timestamp Freshness Verification**: Commands older than 120 seconds or drifting more than 60 seconds into the future are dropped to prevent replay attacks.
* **Device Authorization Whitelist**: Only public keys explicitly authorized during the physical desktop pairing handshake can execute commands. Unknown keys are denied access.

---

## Workstation Security & Rate Limiting

* **Desktop-Exclusive Master PIN**: The 6-digit Master PIN is established on the physical workstation and can only be altered from the physical desktop UI. Remote PIN modification is strictly rejected.
* **PBKDF2-HMAC-SHA256**: PIN hashes are derived using 100,000 iterations and a 16-byte cryptographically secure random salt.
* **Timing-Attack Resistance**: Verification uses constant-time byte comparisons (`subtle.ConstantTimeCompare`).
* **Anti-Brute-Force Lockout**: Five consecutive invalid attempts trigger an automatic 3-minute lockout. Lockout state persists across application restarts.
* **Restricted CORS**: Cross-Origin Resource Sharing on the local server is restricted to authorized origins (official PWA domain and local subnet IPs), eliminating wildcard `*` exposure.

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
* [Inno Setup 6](https://jrsoftware.org/isinfo.php) (only needed for packaging the installer)

### Compilation

```cmd
git clone https://github.com/Venkateshwar-T/PC-Remote.git
cd PC-Remote

REM Run all unit and cryptographic tests
go test -v ./...

REM Build standalone stripped Windows GUI binary (PC-Remote.exe)
scripts\build.bat

REM Build complete installer (dist\PC-Remote-Setup.exe)
scripts\build_installer.bat
```

---

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
