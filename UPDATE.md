Now perform the next security-hardening pass on the CURRENT codebase.

Do not redesign the UI or unrelated Windows functionality. Work from the current implementation that already uses:

- real Nostr secp256k1 keypairs
- Nostr signed events
- NIP-44 E2EE
- multiple public Nostr relays
- authorized phone public keys
- replay protection
- public-entry state machine

The goal now is to close the remaining security gaps without breaking the working architecture.

==================================================
1. PAIRING TOKEN: ACTUAL EXPIRATION + SINGLE USE
==================================================

Current code creates:

pairingToken
pairingExp = time.Now().Add(5 * time.Minute)

but the token expiry is not actually enforced everywhere.

Fix this properly.

Requirements:

- Pairing token must have cryptographically random entropy of at least 128 bits.
- Pairing token must expire after 5 minutes.
- Every validation must check both:
  - token equality
  - current time < pairing expiration
- A successful pairing must consume the token immediately.
- Once consumed, that token must never authorize another device.
- Regenerate a fresh pairing token after successful pairing.
- Prevent race conditions where two phones simultaneously submit the same valid token and both become authorized.
- Token state must be protected by a mutex.
- Do not expose whether a particular token currently exists to unauthenticated clients.

Add tests for:

A. valid token -> accepted
B. expired token -> rejected
C. wrong token -> rejected
D. successful pairing -> token consumed
E. reuse of consumed token -> rejected
F. two concurrent pairing attempts with the same token -> only one may succeed

==================================================
2. REMOVE PLAINTEXT PIN FROM LAN CONTROL
==================================================

This is currently still wrong.

The LAN path in web/app.js sends:

pin
token
phonePubKey

inside a plaintext HTTP request to:

http://<LAN-IP>:8765/api/control

Do NOT send the Master PIN as a normal LAN API field for authenticated commands.

The security architecture must be:

PHONE
  |
  | authenticated + encrypted command
  |
  +---- LAN ----> LAPTOP
  |
  +---- Nostr -> RELAY -> LAPTOP

The transport may differ.
The security protocol must not.

Implement a common authenticated command/envelope format that can be transported through either:

1. direct LAN HTTP
2. Nostr relay

For LAN:

- browser creates the same cryptographically authenticated/encrypted payload used by the relay path
- POST only the cryptographic envelope
- laptop verifies signature
- laptop decrypts
- laptop checks sender authorization
- laptop checks freshness/replay
- laptop executes command

Do not create a weaker "LAN security mode".

The LAN server must reject old-style plaintext PIN command requests after migration, except where an explicitly required first-time pairing path genuinely needs the PIN.

==================================================
3. MASTER PIN MODEL
==================================================

Keep the 6-digit Master PIN.

But use it correctly.

Desired model:

PAIRING:
- user enters Master PIN
- laptop verifies it
- laptop authorizes the phone's public key
- PIN must never become the long-term remote authentication credential

AFTER PAIRING:
- remote commands authenticate using the phone's cryptographic identity
- PIN is not transmitted with commands
- PIN is not required by the laptop for every remote command

PHONE UI:
- the Master PIN should still protect access to the phone dashboard/session if the current UX is intended to require PIN on app unlock

IMPORTANT:
The current already-paired flow appears to set:

config.pin = entered PIN

and then simply sends a cryptographic telemetry request.

That means the laptop may never actually verify that entered PIN.

Fix this.

When the phone is already paired and the user is asked for the Master PIN:

- verify the PIN locally using a secure verifier derived from information established during pairing
- do NOT send the PIN to the laptop just to unlock the UI
- do NOT falsely treat any 6 digits as correct
- do NOT store the plaintext PIN

Use a proper challenge/verifier design appropriate for a 6-digit local unlock secret.

Do not weaken cryptographic remote authentication just to implement the UI lock.

Clearly separate:

LOCAL PHONE UNLOCK
vs.
REMOTE DEVICE AUTHORIZATION

==================================================
4. PROTECT phonePrivkey
==================================================

The current phone private key is stored directly in localStorage.

Do not leave the long-term private key as plaintext localStorage data if a better browser-native architecture is practical.

Preferred design:

- store the private signing key in IndexedDB rather than localStorage
- use Web Crypto where appropriate
- prefer a non-exportable CryptoKey where compatible with the cryptographic implementation
- if the Nostr library requires raw key bytes and non-exportable CryptoKey cannot be used without breaking the required secp256k1/Nostr protocol, do NOT invent an incompatible crypto layer

If raw private-key material must remain browser-accessible because of library limitations:

- minimize how long it lives in JS memory
- never log it
- never place it in URLs
- never place it in relay metadata
- never place it in errors/toasts
- never transmit it directly
- explain the remaining browser-storage limitation accurately

Do not claim that localStorage/IndexedDB is a secure vault.

Treat all client-side storage as attacker-modifiable.

==================================================
5. REMOVE NIP-04 FALLBACK
==================================================

Current relay client attempts:

NIP-44
then NIP-04 fallback

Remove the weaker fallback unless there is a concrete interoperability requirement that cannot otherwise be solved.

PC Remote should use one clearly defined modern encryption protocol.

Use NIP-44 consistently.

Do not silently downgrade security.

Update tests accordingly.

==================================================
6. REPLAY PROTECTION HARDENING
==================================================

Current ReplayGuard checks:

- request ID
- timestamp freshness
- duplicate IDs

Improve it.

Requirements:

- replay identity should be scoped to sender identity, not only a bare request ID
- reject duplicate (sender, requestID)
- reject stale timestamps
- reject excessive future timestamps
- use the Nostr event CreatedAt as well as the inner command timestamp
- do not accept a command if the two timestamps differ beyond a small reasonable tolerance
- keep bounded memory
- prune expired entries deterministically
- do not allow an attacker to grow replay-cache memory without bound

Add tests for:

A. normal command
B. exact replay
C. same request ID from different sender
D. stale event
E. future event
F. mismatched inner timestamp vs event timestamp
G. large number of replay entries
H. concurrent replay checks

==================================================
7. AUTHORIZATION MUST BE SERVER-SIDE
==================================================

Never trust these browser fields:

- isPaired
- laptopPubkey
- phonePubkey
- deviceName
- pairingToken

The browser may modify all of them.

The laptop is the source of truth.

For every remote command:

1. verify Nostr event signature
2. extract authenticated sender public key
3. decrypt payload
4. verify sender public key is authorized
5. verify freshness
6. verify replay state
7. validate action against a strict allowlist
8. execute

Do not use isPaired as an authorization condition on the laptop.

It is UI state only.

==================================================
8. STRICT MESSAGE VALIDATION
==================================================

Before executing anything:

- maximum event size
- maximum ciphertext size
- maximum plaintext size
- maximum device name length
- strict request ID format/length
- strict action enum
- valid timestamps
- no unknown dangerous fields if practical

Do not allow arbitrary command execution.

Allowed actions remain only:

- telemetry
- lock
- sleep
- restart
- shutdown

Keep change_pin desktop-only.

==================================================
9. RESPONSE SECURITY
==================================================

Replies must:

- be signed by the laptop
- be encrypted to the phone
- contain the request ID
- contain fresh timestamp
- never contain the Master PIN
- never contain private keys
- never leak unnecessary internal errors

Phone must reject responses when:

- signature invalid
- sender is not the expected laptop public key
- response ID is not pending
- response is stale
- response has already been processed

==================================================
10. NOSTR EVENT HANDLING
==================================================

Keep real Nostr protocol usage.

Do NOT return to raw custom JSON over a WebSocket.

Review subscription filters and ensure:

- laptop only processes events addressed to its public key
- phone only accepts responses from the paired laptop
- signatures are verified before trust
- only the intended event kind/protocol is processed
- malformed relay data is ignored safely

If the chosen Nostr event kind has a known modern replacement, evaluate it, but do not make a gratuitous protocol migration unless it materially improves PC Remote's security/privacy and remains reliable in browser + Go clients.

==================================================
11. PUBLIC ENTRY STATE MACHINE
==================================================

Preserve the recently implemented public-entry behavior.

Direct visit:

https://pc-remote-45t.pages.dev/

must remain:

No Device Paired

with:

- zero relay connections
- zero LAN requests
- zero telemetry requests
- zero API requests

Malformed pairing URL:
→ Invalid Pairing Link

Expired pairing:
→ Pairing Link Expired

Previously authenticated local state:
→ local PIN unlock screen

Authenticated device but PC unavailable:
→ Device unavailable/reconnecting

Do not let browser-stored isPaired=true bypass actual cryptographic authorization.

==================================================
12. PWA STORAGE / SERVICE WORKER
==================================================

Review sw.js and app startup carefully.

Requirements:

- cached app shell must never imply authentication
- clearing storage must return to unauthenticated state
- stale cached JS must not accidentally reintroduce old insecure protocol behavior if possible
- version cache names correctly
- ensure deployed app updates propagate reliably
- never cache sensitive API responses or authenticated device data as static cache entries

Do not put secrets into the service-worker cache.

==================================================
13. LOCAL SERVER SECURITY
==================================================

Review local_server.go again.

Keep:

- request body limits
- restrictive CORS
- HTTP timeouts
- path traversal protection

Improve where necessary:

- reject malformed methods
- avoid unnecessary information disclosure from /api/status
- do not expose network/device telemetry to unauthenticated callers
- do not accept legacy plaintext command authentication
- ensure Origin/CORS rules are correct
- validate Local Network Access handling without broadening access unnecessarily

==================================================
14. CONFIG FILE SECURITY
==================================================

The laptop private key is currently stored in config.json.

Review this seriously.

Prefer using Windows DPAPI / Windows Credential Manager for the laptop private key if practical.

The long-term laptop private key should not be treated as ordinary JSON configuration if it can be protected by the operating system.

Requirements:

- preserve key across normal restarts
- preserve pairing across normal restarts
- prevent another ordinary user/process from trivially reading the plaintext private key from config.json
- migrate existing installations safely
- never silently destroy an existing valid keypair
- provide a safe fallback if DPAPI/Credential Manager is unavailable

Do not expose private-key material in logs or CLI output.

In particular, reconsider printing the full Nostr public/private identity-related configuration in places where it isn't necessary.

Public key may be displayed.
Private key must never be printed.

==================================================
15. RATE LIMITING
==================================================

Keep PIN brute-force protection.

Also consider:

- pairing attempt rate limiting
- invalid-event rate limiting
- per-sender throttling
- malformed-event throttling

Do not let a malicious relay feed the PC a huge stream of expensive decrypt/verify operations.

Bound concurrency and expensive cryptographic work.

==================================================
16. PERFORMANCE
==================================================

Do not optimize by forcing artificially low RAM usage.

Review the existing:

debug.SetGCPercent(20)
debug.FreeOSMemory()
SetProcessWorkingSetSize(...)

behavior.

Prefer normal Go runtime behavior unless profiling demonstrates a real problem.

The daemon should optimize for:

- low idle CPU
- stable memory
- low network usage
- low latency
- no goroutine leaks
- no busy reconnect loops
- bounded cryptographic work

Do not chase an arbitrary "10 MB" Task Manager number.

==================================================
17. TESTING
==================================================

Actually run:

go test -v ./...
go vet ./...

Also run the JavaScript state tests.

Add tests for every security requirement above.

In particular verify:

A. PIN never appears in relay event plaintext
B. PIN never appears in non-pairing remote commands
C. LAN uses the same E2EE/authentication model
D. expired pairing token rejected
E. pairing token is single-use
F. duplicate command rejected
G. stale command rejected
H. unauthorized phone rejected
I. modified event rejected
J. modified ciphertext rejected
K. random public visitor creates no connections
L. fake URL values do not create a trusted device
M. deleting/modifying local storage does not authorize a device
N. wrong local PIN does not unlock an already-paired session
O. private keys are not logged
P. service-worker cache cannot bypass authentication
Q. multiple relays can fail without breaking the daemon
R. no unbounded goroutine/memory growth from relay traffic

==================================================
18. DOCUMENTATION ACCURACY
==================================================

Update README security claims to match the implementation exactly.

Do not claim:

- "unhackable"
- "perfect security"
- "anonymous"
- "zero metadata"
- "relay sees nothing"

unless technically true.

Accurately document:

- E2EE
- signed commands
- authorized phone keys
- PIN role
- replay protection
- relay-visible metadata
- LAN vs internet transport
- browser storage limitations
- pairing expiration
- revocation behavior

==================================================
19. IMPORTANT IMPLEMENTATION RULE
==================================================

Do not make superficial changes just to satisfy tests.

I want the security model to be coherent:

              PHONE
                |
        local PIN unlock
                |
        phone private key
                |
        sign + encrypt
                |
        +-------+-------+
        |               |
       LAN            Nostr
        |               |
        +-------+-------+
                |
              LAPTOP
                |
         verify signature
                |
         decrypt payload
                |
        authorized key check
                |
          replay/freshness
                |
        strict action allowlist
                |
           Win32 action

The PIN protects the user's ability to use the phone client.
The cryptographic phone identity authorizes the phone to control the PC.
The laptop remains the ultimate security authority.

Do not create a second weaker authentication protocol for LAN.

At the end, report:

1. exact files changed
2. exact security model implemented
3. how PIN is used
4. how phone authentication works
5. how LAN and relay transports share the same security protocol
6. how pairing expiration and single-use work
7. how private keys are stored on Windows
8. how phone private keys are stored in the browser
9. replay-protection design
10. what the relay can see
11. what the relay cannot see
12. tests actually executed and their results
13. any remaining limitations

Do not merely describe proposed changes. Implement them and verify them.