TASK: Convert PC Remote into a proper Windows boot-time background service + separate interactive tray/UI process.



IMPORTANT:

This is an architectural change, but preserve the existing product behavior, cryptographic protocol, PWA, relay architecture, LAN architecture, security model, and UI wherever possible.



Do NOT simply make the current GUI process an interactive Windows service.



Windows services cannot directly interact with the interactive desktop. The service and tray/UI must be separate responsibilities.



==================================================

1\. PRIMARY REQUIREMENT

==================================================



CURRENT BEHAVIOR:



PC Remote currently starts through the user's Windows Startup folder.



Therefore after reboot:



Windows boot

→ Windows login screen

→ user enters Windows PIN/password/fingerprint

→ PC Remote starts

→ remote control becomes available



DESIRED BEHAVIOR:



PC reboot

→ Windows boots

→ PC Remote BACKGROUND SERVICE starts automatically

→ Nostr relay connections establish

→ LAN HTTP server starts

→ remote commands can already reach the PC

→ Windows may still be sitting at the login screen

→ remote Lock/Sleep/Restart/Shutdown/Telemetry functionality remains available



After the user logs into Windows:



→ PC Remote TRAY/UI starts

→ pairing QR window/settings/interactive features become available



Also preserve:



Logged-in Windows + Win+L locked

→ service remains running

→ remote control remains available



This is the required final behavior.



==================================================

2\. DO NOT TURN THE CURRENT GUI INTO A SERVICE

==================================================



Do NOT run the existing tray/UI/message-loop code directly as a Windows service.



Windows services run outside the interactive desktop/session.



Create a clean separation:



A. PC Remote Service

B. PC Remote Tray/UI



The service owns all functionality necessary for remote operation.



The tray owns only interactive desktop functionality.



==================================================

3\. SERVICE RESPONSIBILITIES

==================================================



The Windows service MUST own:



\- Nostr relay connections

\- NIP-44 cryptographic processing

\- laptop cryptographic identity

\- protected laptop private key

\- pairing-token state

\- Master PIN verifier

\- authorized phone public-key allowlist

\- replay protection

\- command protocol handler

\- LAN /api/control server

\- telemetry generation

\- Lock

\- Sleep

\- Restart

\- Shutdown

\- service lifecycle

\- persistence of security-sensitive state

\- rate limiting/concurrency protection



The service must remain fully functional when:



\- no user is logged in

\- Windows is sitting at the login screen

\- the interactive user session does not yet exist



Do NOT make the remote-control path depend on the tray process.



==================================================

4\. TRAY/UI RESPONSIBILITIES

==================================================



The tray/UI process should own:



\- system tray icon

\- pairing QR display

\- pairing window

\- desktop PIN setup/change UI

\- opening the PWA

\- desktop notifications

\- interactive settings/UI



The tray must communicate with the service through LOCAL IPC only.



Do NOT communicate between tray and service over HTTP.

Do NOT expose an additional TCP control port for tray↔service communication.



Preferred IPC:

\- Windows Named Pipe



Example conceptual pipe:



\\\\.\\pipe\\PCRemote



Use a dedicated, strongly restricted security descriptor / DACL.



The pipe must NOT be remotely accessible over SMB/network.



Microsoft documents that named pipes are securable through ACLs and that local-only use should explicitly prevent remote access. Use an appropriate ACL and deny network access. Do not leave the default permissive pipe security descriptor. 



The service must validate the caller's Windows identity/SID where appropriate.



==================================================

5\. IPC SECURITY MODEL

==================================================



Do not make the tray an alternative authority.



The service remains the sole authority.



The tray should only request operations such as:



\- GetPairingInfo

\- Show/refresh pairing information

\- Get device status

\- Request PIN setup state

\- Set/change PIN

\- Request service status

\- Request controlled shutdown of service if explicitly supported



Never allow the pipe to bypass the normal security model.



Do NOT create IPC methods such as:



"execute arbitrary command"



"run arbitrary PowerShell"



"run arbitrary exe"



"execute lock without validation"



The IPC interface must have a strict allowlist of operations.



Authenticate the local IPC caller using Windows security context/SID.



Prefer restricting the pipe to the Windows account that owns the PC Remote installation/configuration.



Do not give arbitrary local network users access.



==================================================

6\. CRYPTOGRAPHIC KEY STORAGE — CRITICAL

==================================================



CURRENT CODE:



The laptop private key uses normal Windows DPAPI user-scoped protection.



That currently works because the existing application runs in the logged-in user's context.



Do NOT simply move the current DPAPI blob into a LocalSystem service.



The service account context is different.



Design proper service-owned secret storage.



Preferred architecture:



\- Run the background service under the least-privileged suitable built-in Windows service account, preferably LocalService unless a stronger reason requires another account.

\- Do NOT use LocalSystem unless absolutely necessary.

\- Do NOT require storing the user's Windows password.

\- Do NOT ask the user for their Windows password just to install the service.

\- Do NOT use CRYPTPROTECT\_LOCAL\_MACHINE merely as a shortcut without considering its security consequences.



Use service-account-scoped DPAPI for the service-owned secret where appropriate.



The service should generate/store/decrypt its long-term laptop private key in its own security context.



The encrypted key file must be protected with filesystem ACLs so ordinary users cannot replace/read/modify it.



The service must own the authoritative copy of the laptop private key.



==================================================

7\. EXISTING INSTALLATION / MIGRATION

==================================================



Preserve existing users where reasonably possible.



CURRENT INSTALLATIONS may have:



config.json

containing an existing user-scoped DPAPI-protected laptop private key.



Implement a safe migration path.



Preferred migration flow:



1\. During upgrade/first service installation, the existing user-session process can decrypt the old user-scoped DPAPI private key.

2\. Pass the plaintext key ONLY through a secure local IPC mechanism to the newly installed service.

3\. Service validates the keypair.

4\. Service re-encrypts/protects it using its own service-context protection.

5\. Service persists the protected key in its service-owned configuration.

6\. Plaintext key must never be written to disk.

7\. Never put the private key in command-line arguments.

8\. Never put the private key in environment variables.

9\. Never put the private key in logs.

10\. Never transmit the private key over the network.



After successful migration, securely remove the obsolete plaintext/old-format storage.



If seamless migration is impossible in a safe manner, fail clearly and preserve the old configuration rather than silently destroying identity.



DO NOT silently generate a new laptop identity merely because the service cannot access the old one.



==================================================

8\. SERVICE CONFIGURATION LOCATION

==================================================



Do not keep security-sensitive service state next to a per-user GUI executable if that creates weak filesystem permissions.



Choose a proper machine/application-data location, such as ProgramData, with explicit ACLs.



Service configuration should be inaccessible for modification by unprivileged users.



At minimum, protect:



\- laptop private key ciphertext

\- PIN verifier

\- PIN salt

\- authorized device list

\- pairing state



The service must be the authoritative configuration owner.



Do not let the tray directly edit config files.



==================================================

9\. PAIRING SYSTEM

==================================================



Preserve the existing:



\- 128-bit cryptographically secure pairing token

\- 5-minute expiration

\- atomic single-use consumption

\- race protection



The service owns this state.



The tray requests the current pairing URL/token from the service through secure IPC.



CRITICAL:



The pairing QR must always represent the current pairing token.



Do not cache the pairing URL forever.



After token rotation:



\- service produces fresh pairing data

\- tray refreshes the QR

\- "Show Pairing QR Code" always displays current information



The service must be able to rotate pairing state even though the tray is not running.



==================================================

10\. SERVICE STARTUP

==================================================



Install the PC Remote daemon as a real Windows service.



Requirements:



\- Service type: own process

\- Start type: SERVICE\_AUTO\_START

\- Run without interactive user login

\- Report proper service states to SCM

\- Properly handle START\_PENDING

\- Transition to RUNNING only after essential initialization

\- Respond to STOP

\- Respond to SHUTDOWN

\- Stop network listeners cleanly

\- Close Nostr connections cleanly

\- Persist required state before shutdown when necessary



Do not perform long/unbounded work before registering with SCM.



Follow proper ServiceMain/service-control-handler behavior.



If initialization takes time, report progress/status appropriately.



==================================================

11\. SERVICE RECOVERY

==================================================



Configure SCM service recovery so unexpected service crashes do not permanently disable remote control.



Preferred recovery:



First failure:

restart service



Second failure:

restart service



Subsequent failure:

restart service



Use reasonable reset periods.



Do not configure catastrophic behavior such as system reboot when PC Remote crashes.



Do not create a restart loop that consumes CPU aggressively.



==================================================

12\. TRAY STARTUP

==================================================



After introducing a boot-time service:



Do not use the tray application as the mechanism that makes remote control available.



The service starts at boot.



The tray starts after user login.



The tray may be registered using the per-user startup mechanism, or an equivalent user-session startup mechanism.



When tray starts:



\- connect to the local service

\- query service state

\- query current pairing information

\- display the pairing UI if required

\- do not duplicate the network daemon



Do not start a second Nostr client from the tray.



Do not start a second LAN server from the tray.



Do not create duplicate cryptographic state.



==================================================

13\. MAIN PROCESS ARCHITECTURE

==================================================



Refactor the current main entry point cleanly.



Use explicit modes if a single executable is preferable, for example conceptually:



PC-Remote.exe service

PC-Remote.exe tray



or separate service/tray executables if that is cleaner.



Do NOT rely on fragile detection of "running as service" based purely on environment guesses.



The service entry point must properly integrate with Windows SCM.



The tray entry point must only initialize interactive UI.



Keep shared protocol/config/security packages reusable between the two.



Avoid duplicated security logic.



==================================================

14\. LAN SERVER

==================================================



The LAN HTTP server must run inside the SERVICE.



It must therefore be alive before the user logs into Windows.



Preserve:



\- signed Nostr event envelope

\- NIP-44 encryption

\- server-side phone public-key authorization

\- replay protection

\- request size limits

\- strict action allowlist

\- LAN concurrency limit

\- rate limiting

\- current CORS restrictions



Do not create a separate insecure service endpoint for LAN control.



==================================================

15\. NOSTR RELAY

==================================================



The Nostr relay client must run in the SERVICE.



It must work before user login.



Preserve:



\- current three-relay architecture

\- NIP-44 v2 only

\- BIP-340 signatures

\- event verification

\- destination tag validation

\- replay protection

\- bounded relay worker concurrency

\- response encryption/signing



Do not move relay networking into the tray.



==================================================

16\. WINDOWS ACTIONS

==================================================



Verify each Windows operation from service context:



\- telemetry

\- Lock Workstation

\- Sleep

\- Restart

\- Shutdown



Important:



Do not assume interactive desktop APIs behave the same from Session 0.



Test Lock Workstation specifically while no interactive user is logged in and while an interactive session is locked.



Use the correct Windows APIs for service context.



Do not allow the service to create an interactive service desktop/window.



Windows explicitly separates services from interactive sessions.



==================================================

17\. MASTER PIN

==================================================



The Master PIN remains:



\- 6 numeric digits

\- set/changed through the desktop UI

\- verified by the SERVICE for initial pairing

\- never transmitted as plaintext remote command authentication

\- not required for each remote command



After pairing:



phone cryptographic identity authorizes remote commands.



Do not move Master PIN handling back into the tray as an authority.



The tray may ask the service to set/change the PIN through authenticated local IPC.



==================================================

18\. INSTALLER REDESIGN

==================================================



The current installer is per-user oriented.



Change it appropriately for a real boot-time service.



A system service requires administrative installation.



The installer should:



1\. install service binary to a protected machine-wide location

2\. install any required tray binary/components

3\. create necessary ProgramData/config directories

4\. set secure filesystem ACLs

5\. register PC Remote service with SCM

6\. configure SERVICE\_AUTO\_START

7\. configure service recovery

8\. start service after installation

9\. install tray startup entry for the logged-in installing user

10\. optionally start tray immediately after installation



Do not put service configuration in the user's Startup folder.



Do not use task scheduler as a substitute for the service unless there is a compelling Windows compatibility reason.



The installer should clearly explain that PC Remote's background service starts with Windows so remote control remains available before login.



Suggested wording:



"Run PC Remote in the background automatically with Windows"



Description:



"Allows your PC to remain remotely reachable after reboot, even before you sign in to Windows."



==================================================

19\. UNINSTALL / UPGRADE

==================================================



Handle service lifecycle safely.



Before uninstall:



\- stop service

\- wait for confirmed STOPPED state

\- unregister/delete service

\- stop tray process

\- remove tray startup entry

\- remove application binaries



Do not use unsafe blanket taskkill by process name if that could terminate an unrelated process with the same filename.



Prefer SCM-controlled service stop and precise process management.



For configuration/data:



Decide explicitly whether user configuration is removed.



Do not silently erase security identity on every upgrade.



For uninstall, provide deliberate data behavior.



Do not automatically delete user data merely because the binary is being upgraded.



==================================================

20\. FIREWALL / NETWORKING

==================================================



Verify the service can:



\- establish outbound WSS relay connections before user login

\- bind the LAN HTTP port before user login



Check whether Windows Firewall requires an explicit rule for LAN direct mode.



If a firewall rule is needed, create the narrowest appropriate rule:



\- only the required executable/service

\- only required port

\- preferably local/private network scope

\- no unnecessary public exposure



Do not create broad "allow all inbound TCP" rules.



==================================================

21\. SERVICE ACCOUNT / PRIVILEGE MINIMIZATION

==================================================



Use least privilege.



Prefer LocalService where technically sufficient.



Do not run as LocalSystem simply because it is easier.



The service should not require:



\- arbitrary process creation

\- shell execution

\- PowerShell execution

\- administrative token for ordinary telemetry

\- arbitrary filesystem access



Grant only permissions actually required for:



\- network access

\- Windows power operations

\- required telemetry APIs

\- service-owned configuration

\- IPC



Document any privilege that cannot be avoided.



==================================================

22\. SECURITY OF THE TRAY ↔ SERVICE PIPE

==================================================



The named pipe is security-sensitive.



Do NOT expose:



\\\\.\\pipe\\PCRemote



with default broad permissions.



Create an explicit security descriptor.



Prevent remote/network access.



Restrict access to the intended local Windows user/account(s).



Validate the client's Windows identity where sensitive operations are involved.



Reject malformed requests.



Bound message sizes.



Use request/response correlation IDs if needed.



Do not let a malicious local process invoke privileged service operations simply because it can connect to the pipe.



==================================================

23\. DO NOT BREAK CURRENT CRYPTO ARCHITECTURE

==================================================



Keep:



\- NIP-44 v2

\- BIP-340 Schnorr

\- Nostr Event Kind 4

\- recipient p-tag

\- phone public-key authorization

\- single-use pairing token

\- replay protection

\- signed/encrypted response events

\- phone IndexedDB/Web Crypto vault

\- public PWA state machine

\- LAN/relay protocol parity



Do not reintroduce:



\- plaintext PIN over LAN

\- PIN on every remote request

\- NIP-04

\- unauthenticated remote commands

\- browser localStorage private-key storage

\- arbitrary local control endpoint



==================================================

24\. PUBLIC PWA

==================================================



The public Cloudflare Pages PWA remains a client.



A random visitor:



https://pc-remote-45t.pages.dev/



must still receive:



"No Device Paired"



and:



\- zero relay connections

\- zero LAN requests

\- no dashboard

\- no PIN-unlock flow unless valid pairing/session context exists



Do not make the service architecture change alter this state machine.



==================================================

25\. PERFORMANCE / STABILITY

==================================================



The service must remain lightweight.



Avoid:



\- unbounded goroutines

\- unbounded queues

\- busy polling

\- repeated config writes

\- duplicate relay connections

\- duplicate LAN servers

\- duplicate tray processes



Preserve the current relay concurrency bounds.



Preserve the current LAN concurrency bounds.



Use graceful shutdown.



Do not reintroduce artificial GC/working-set hacks just to make memory numbers look smaller.



==================================================

26\. TESTING REQUIREMENTS

==================================================



Add real tests for the service architecture.



At minimum:



A. Service starts successfully without a logged-in user where testable

B. Service initializes relay client

C. Service initializes LAN server

D. Service reads protected configuration

E. Service can decrypt its own protected laptop private key

F. Service rejects unauthorized commands

G. Service accepts authorized commands

H. Replay protection remains functional

I. Pairing token remains single-use

J. QR information comes from current token

K. Tray communicates through secured local IPC

L. Unauthorized local IPC client is rejected

M. Malformed IPC requests are rejected

N. Service stop is graceful

O. Service restart recovers normally

P. Upgrade/migration preserves laptop identity where possible

Q. No private key appears in logs

R. No private key appears in command-line arguments

S. No plaintext PIN appears in network command payloads



Run:



go test -count=1 ./...

go test -race ./...

go vet ./...

node tests/state\_test.js



If Windows-specific service tests require Windows, execute those on Windows and report the actual result.



==================================================

27\. MANUAL ACCEPTANCE TEST

==================================================



After implementation, explicitly verify this sequence on a real Windows installation:



TEST 1:

Install PC Remote.



TEST 2:

Restart Windows.



TEST 3:

Stop at the Windows PIN/password/fingerprint login screen.



DO NOT LOG IN.



From an already-paired phone using mobile data:

send telemetry.



Expected:

PC Remote responds.



Then:

send Lock.



Expected:

command is accepted as appropriate for the current Windows session state.



Then:

log into Windows.



Expected:

PC Remote tray appears.



Then:

press Win+L.



Expected:

PC Remote service remains alive and remote telemetry/control still works.



Then:

log back in.



Expected:

tray communicates with the already-running service.



==================================================

28\. IMPORTANT FAILURE RULE

==================================================



Do not claim:



"service works before login"



unless this has actually been tested on Windows.



Do not claim:



"existing installations migrate safely"



unless the migration path has actually been tested.



Do not claim:



"private key is protected"



unless the service-context storage and filesystem ACLs have actually been inspected/tested.



==================================================

29\. IMPLEMENTATION STYLE

==================================================



This is a production architecture change.



Do not produce a giant pile of duplicated code.



Reuse the current internal packages.



Prefer clear boundaries:



internal/service/

internal/ipc/

internal/config/

internal/protocol/

internal/relay/

internal/server/

internal/tray/



or an equivalent clean structure.



Keep platform-specific Windows service code isolated from protocol/business logic.



Keep interactive UI code isolated from service code.



Do not modify unrelated UI styling.



==================================================

30\. FINAL REPORT

==================================================



After implementation provide:



1\. Exact files created/modified

2\. New process architecture

3\. Service account used and why

4\. Key-storage design and migration strategy

5\. IPC security design

6\. Installer changes

7\. Uninstall/upgrade behavior

8\. Tests actually executed

9\. Manual Windows acceptance test results

10\. Any remaining limitations



Do not claim completion without actually building and testing the service.



The end result must satisfy this exact requirement:



REBOOT

↓

Windows boot

↓

PC Remote SERVICE starts

↓

Nostr + LAN + crypto initialized

↓

Windows still at PIN/password/fingerprint login screen

↓

PHONE CAN REMOTELY CONTROL PC



Then:



USER LOGS IN

↓

PC Remote TRAY starts

↓

QR/settings/UI available



The service is the always-on remote-control component.

The tray is only the interactive desktop component.

