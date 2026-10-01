Now make one focused security/UX change to the public PWA entry point.

CURRENT ISSUE / REQUIREMENT:

The Cloudflare Pages PWA is publicly accessible at:

https://pc-remote-45t.pages.dev/

A random person can visit that URL directly without having a PC Remote pairing QR code.

That person must NOT see the PIN keypad, dashboard, telemetry, controls, or any device connection attempt.

Do NOT treat this as a network failure and do NOT show misleading messages such as:
- "Request timed out"
- "Connection failed"
- "Unable to connect"

A random visitor has not attempted to connect to anything. They simply have no pairing context.

IMPLEMENT THIS BEHAVIOR:

1. DIRECT PUBLIC VISIT

When the page is opened without valid pairing information:

Show a dedicated neutral "No Device Paired" / "Pairing Required" state.

Example:

PC Remote

No device paired

Open this page from the pairing QR code
displayed by PC Remote on your Windows PC.

Do not:
- show PIN keypad
- initialize LAN communication
- initialize relay communication
- poll telemetry
- call `/api/control`
- call `/api/status`
- attempt reconnect loops

The page should remain completely idle.

2. PAIRING DATA MUST NOT BE TRUSTED JUST BECAUSE IT EXISTS

The current QR flow places values such as:
- pairing token
- laptop public key
- LAN address
- device name

into the URL fragment.

Do NOT consider a page "paired" merely because `pair`, `key`, `lan`, or `name` exist.

Those values can be manually fabricated.

The pairing state must only transition into an authenticated device session after the new cryptographic pairing/authentication system implemented in the previous task has successfully validated the device.

Use the new protocol implemented in the previous task rather than creating a second unrelated authentication mechanism.

3. INVALID / MALFORMED PAIRING DATA

If URL pairing parameters are present but malformed, incomplete, invalid, cryptographically unverifiable, or otherwise unusable:

Show:

"Invalid pairing link"

"Generate a new pairing QR code from PC Remote."

Do not attempt control requests.

Do not expose raw errors, stack traces, relay errors, or internal protocol details.

4. EXPIRED PAIRING

If the pairing token/challenge is structurally valid but expired:

Show:

"Pairing link expired"

"Generate a new pairing QR code from PC Remote."

Do not continue to the PIN screen or dashboard.

5. VALID PAIRING BUT DEVICE OFFLINE

This is different from an invalid public visit.

Once a pairing has been genuinely established/authenticated, the UI may show:

"Device unavailable"

"Waiting for the PC Remote connection..."

At that point the app may use the LAN/relay reconnection logic.

Do not confuse this state with the unauthenticated public landing page.

6. AUTHENTICATED SESSION

Only after successful cryptographic authentication/pairing:

Show the existing PIN/authentication/dashboard flow as appropriate.

Do not break the existing authenticated functionality.

7. SECURITY / PRIVACY

A random visitor must not be able to discover:
- a laptop's telemetry
- network information
- device identity beyond intentionally public static application information
- whether a specific laptop is online
- whether a specific pairing token exists
- relay identifiers or internal connection details

Do not make API calls merely to determine whether something is paired.

The unauthenticated landing state should be client-side and deterministic whenever possible.

8. URL CLEANUP

Continue cleaning sensitive pairing fragments from the visible browser URL after successful parsing, but make sure this does not accidentally convert an unauthenticated visitor into a paired session.

Do not persist invalid pairing data.

Do not persist arbitrary attacker-supplied `key`, `pair`, `lan`, or `name` values as trusted device configuration.

9. SERVICE WORKER / PWA

The public PWA should still load normally.

Offline caching should not accidentally expose a previously authenticated dashboard to a completely unrelated visitor/device.

Review the current service-worker behavior and ensure cached application state does not bypass authentication.

10. UX

Keep the existing visual language and UI design.

Do not redesign the application.

Add only the necessary state/view and state-management logic.

The result should feel like:

PUBLIC VISIT
    ↓
Pairing Required

VALID QR
    ↓
Validate/authenticate pairing
    ↓
Authentication flow

AUTHENTICATED DEVICE
    ↓
Dashboard

AUTHENTICATED DEVICE + OFFLINE PC
    ↓
Device unavailable / reconnecting

11. TEST THESE CASES

Test all of the following:

A. `https://pc-remote-45t.pages.dev/`
→ Pairing Required

B. URL with random fake `pair` + `key`
→ Invalid pairing link
→ zero network/control attempts

C. Expired legitimate pairing data
→ Pairing link expired

D. Valid legitimate pairing
→ existing pairing/authentication flow

E. Previously authenticated device
→ existing behavior remains intact

F. Authenticated device whose laptop is offline
→ Device unavailable / reconnecting

G. Clear browser storage and revisit public URL
→ Pairing Required

H. Open public URL in a different browser/device
→ Pairing Required

I. Ensure cached PWA data cannot directly open the dashboard without authentication.

Do not modify unrelated functionality.

At the end, report:
1. exact files changed
2. new application states added
3. how unauthenticated public visits are detected
4. how fake pairing parameters are handled
5. how expired pairing is handled
6. how cached PWA state is prevented from bypassing authentication
7. tests performed and their results