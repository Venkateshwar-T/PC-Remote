Review the current PC Remote codebase at the latest commit and implement ONLY the following focused hardening/fix pass.



IMPORTANT:

\- Do NOT change the Windows service account from LocalSystem to LocalService in this pass.

\- Do NOT redesign the current service architecture.

\- Preserve all currently working functionality.

\- Do NOT perform unrelated refactors or large rewrites.

\- Keep the implementation production-quality, secure, and performance-conscious.



==================================================

1\. HARDEN WINDOWS NAMED-PIPE AUTHORIZATION

==================================================



Current issue:



internal/ipc/pipe\_windows.go currently allows:



&#x20;   (A;;GRGW;;;IU)



where IU = Interactive Users.



This is too broad because the IPC interface exposes privileged service operations.



Privileged IPC methods currently include:



\- SetPin

\- RotatePairingToken

\- MigrateLegacyKey

\- StopService

\- GetPairingInfo



GetPairingInfo must also be treated as sensitive because it exposes pairing information/capability.



Required change:



\- Remove broad Interactive Users authorization.

\- Restrict IPC access to the intended local Windows user/session using a robust Windows identity mechanism.

\- Prefer Windows SID/logon-SID based authorization or another equivalent Windows-native identity control.

\- Do NOT build unnecessary multi-user support.

\- The current intended deployment is a single Windows user.

\- The existing tray for the intended user must continue communicating normally with the service.

\- An unrelated local Windows account must not be able to use the privileged IPC interface.



Do not rely only on the pipe name being local.



Use Windows security APIs/ACLs correctly rather than string-based assumptions.



Also review whether the service should verify the connecting client's Windows identity at the IPC server layer in addition to the pipe ACL. Add identity validation if needed for defense in depth.



Add focused tests or validation for the authorization mechanism where technically possible.



Do not claim multi-user behavior was tested unless it was actually tested with multiple accounts.





==================================================

2\. MAKE LEGACY CONFIG MIGRATION SAFE

==================================================



Review the existing tray -> service legacy migration flow.



Migration must be atomic and idempotent.



Required behavior:



1\. Read and validate the legacy configuration.

2\. Validate all data before modifying the new service configuration.

3\. Transfer supported configuration safely.

4\. Write the new service configuration atomically.

5\. Verify the resulting service configuration can be successfully loaded/decrypted.

6\. Only after successful verification, rename/archive the legacy configuration.

7\. If the process crashes or is interrupted, retrying migration must be safe.

8\. Never leave the application believing migration succeeded when the new configuration was not actually committed.

9\. Never log private keys, PINs, hashes, salts, or other secrets.



Preserve existing configuration wherever the legacy format supports it.



In particular, review whether migration currently preserves only the laptop private key while silently losing things such as:



\- PIN configuration

\- PIN salt/hash

\- device name

\- authorized devices

\- other compatible persistent state



Preserve compatible fields rather than unnecessarily forcing the user to reconfigure/re-pair.



A separate MigrationComplete state/field is NOT required if the migration algorithm itself is correctly atomic and idempotent.



Do not add migration-state machinery just for appearance.





==================================================

3\. FAIL CLOSED IF CONFIGURATION SECURITY SETUP FAILS

==================================================



Review:



internal/config/config.go



Especially:



SetupDirectorySecurity(...)



Current behavior must not silently continue if the security ACL operation fails.



Required:



\- Do not ignore icacls/ACL errors.

\- If directory security cannot be applied or verified, return an error.

\- Daemon.Start() must not continue operating with potentially unsafe service configuration permissions.

\- Fail safely/closed.

\- Verify the security operation actually succeeded rather than assuming command execution was sufficient.

\- Preserve the existing %ProgramData%\\PC Remote service-owned configuration location.



Do not loosen permissions simply to avoid startup failure.





==================================================

4\. FIX THE INITIAL UI FLASH / WRONG-FIRST-PAINT BUG

==================================================



Current problem:



web/index.html renders:



&#x20;   <div id="pinView" class="pin-view">



as visible by default.



Then web/app.js asynchronously executes:



&#x20;   evaluateInitialState()



which calls loadVaultAuth(), including asynchronous IndexedDB access.



As a result, on a completely public visit:



1\. HTML loads.

2\. PIN view is initially visible.

3\. JavaScript starts.

4\. IndexedDB lookup occurs asynchronously.

5\. Browser can paint the PIN screen.

6\. evaluateInitialState() later determines there is no paired device.

7\. "No Device Paired" replaces the PIN view.



This creates a brief visible PIN-screen flash on refresh.



Fix the initialization architecture so the user NEVER sees an incorrect application state before initial state determination is complete.



Required final behavior:



PUBLIC VISITOR:

&#x20;   Page opens

&#x20;   -> No Device Paired



VALID QR / PAIRING URL:

&#x20;   Page opens

&#x20;   -> Pairing PIN screen



INVALID QR:

&#x20;   Page opens

&#x20;   -> Invalid Pairing Link



EXPIRED QR:

&#x20;   Page opens

&#x20;   -> Pairing Link Expired



EXISTING PAIRED SESSION:

&#x20;   Page opens

&#x20;   -> Local PIN unlock screen



There must be no visible flash of:



\- PIN screen for a public visitor

\- dashboard for a public visitor

\- dashboard for a locked paired user

\- stale UI from a previous application state

\- incorrect action state



Important:



\- Do NOT solve this with arbitrary setTimeout delays.

\- Do NOT add a noticeable loading screen.

\- Do NOT make initial loading unnecessarily slow.

\- Do NOT start Nostr relay connections merely to determine whether the visitor is paired.

\- Keep the existing deterministic state-machine behavior.

\- The app should reveal the selected final state only after initialization has completed.



A clean approach such as an explicit boot/initializing state with CSS/DOM visibility is acceptable.



The important invariant is:



&#x20;   No incorrect state is paintable before initialization completes.



Make sure the solution works on both:

\- first visit

\- refresh/reload





==================================================

5\. PRESERVE THE EXISTING SECURITY MODEL

==================================================



Do NOT regress the security architecture already implemented.



Keep:



\- Master PIN used for pairing and local phone unlock.

\- Long-term authorization based on the phone cryptographic identity.

\- NIP-44 v2 only.

\- Signed Nostr events.

\- Replay protection.

\- No plaintext PIN transmitted over LAN.

\- DPAPI protection for the laptop private key.

\- Secure pairing-token generation.

\- Pairing-token expiration.

\- Pairing-token single-use behavior.

\- Existing local/direct-LAN and Nostr relay architecture.



Do NOT:



\- reintroduce NIP-04

\- add plaintext PIN transport

\- weaken authorization to make IPC compatibility easier

\- expose privileged service operations to arbitrary clients





==================================================

6\. TESTING REQUIREMENTS

==================================================



Run the existing automated test suite after the changes.



Add focused tests where practical for:



\- IPC authorization

\- migration safety/idempotence

\- configuration ACL failure behavior

\- initial application-state selection



For the browser issue specifically verify that the PIN view is not visible before initialization determines that the user is paired and locked.



For Windows service/IPC behavior:



\- Verify the intended existing user can still communicate with the service.

\- Verify the pipe is no longer broadly accessible to Interactive Users.

\- Verify the service still starts and the tray still works.



IMPORTANT:



Do NOT claim that shutdown/restart or pre-login behavior was fully tested unless an actual Windows shutdown/reboot was performed.



LocalSystem remains unchanged in this pass.



Do NOT attempt to test reboot by automatically restarting the development machine.





==================================================

7\. CODE QUALITY / SCOPE CONTROL

==================================================



Keep changes focused.



Before editing:



\- inspect the existing implementation and understand the current architecture;

\- reuse existing helpers where appropriate;

\- avoid duplicate security logic;

\- avoid unnecessary abstractions;

\- avoid unnecessary dependencies;

\- avoid large increases in code size;

\- preserve existing UI and UX unless required for the initialization bug.



After editing:



\- run formatting;

\- run tests;

\- check for race conditions;

\- check for error-path regressions;

\- check that sensitive values are not logged;

\- review the final diff for unrelated changes.





==================================================

FINAL REPORT

==================================================



At the end, report:



1\. Files changed.

2\. Exact named-pipe authorization model now used.

3\. How legacy migration is now made atomic/idempotent.

4\. How configuration ACL failures are handled.

5\. How the initial UI flash was eliminated.

6\. Tests actually executed and their real results.

7\. Anything that could not be physically tested.



Do not claim success for tests that were not actually run.

Do not claim multi-user testing that was not performed.

Do not change LocalSystem in this task.

