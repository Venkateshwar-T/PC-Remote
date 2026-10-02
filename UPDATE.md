You are working directly inside the existing PC Remote repository.



IMPORTANT:

Do NOT treat this as a greenfield project.

First inspect the entire existing codebase and understand the current architecture, especially:



\- cmd/laptopcontrol/main.go

\- internal/service/\*

\- internal/ipc/\*

\- internal/config/\*

\- internal/tray/\*

\- internal/win32/\*

\- web/\*

\- installer/\*

\- existing tests

\- go.mod / package structure

\- current Nostr protocol and cryptography

\- current pairing/authentication flow

\- current LAN transport

\- current telemetry implementation

\- current dashboard navigation/state management



The repository already has an established Windows service + tray + web client architecture.

Preserve that architecture and existing functionality.



DO NOT rewrite unrelated parts of the application.

DO NOT replace the existing security model with a simpler one.

DO NOT introduce a separate vendor backend.

DO NOT add a cloud service, paid API, proprietary remote-access service, or mandatory hosted infrastructure.



==================================================

FEATURE TO IMPLEMENT

==================================================



Add a new "Remote Desktop" capability to PC Remote.



The feature must allow the already-paired phone/browser to:



1\. View the Windows PC screen live.

2\. Move the Windows mouse.

3\. Left click.

4\. Right click.

5\. Middle click.

6\. Scroll.

7\. Eventually support keyboard input cleanly, but keyboard UI is NOT required in this first implementation unless the existing architecture makes it trivial and clean.



The first release of this feature is intentionally Windows + browser/mobile-web only.



Android APK support will come later.



The existing web application remains the primary client for now.



==================================================

HIGH-LEVEL ARCHITECTURE

==================================================



Use this architecture:



Browser/PWA

&#x20;   |

&#x20;   | Nostr signaling only

&#x20;   v

PC Remote Windows Service

&#x20;   |

&#x20;   | authenticated local IPC

&#x20;   v

Interactive-session Remote Desktop Worker

&#x20;   |

&#x20;   +--> Windows Desktop Duplication API

&#x20;   |        |

&#x20;   |        +--> D3D11

&#x20;   |        +--> DXGI

&#x20;   |

&#x20;   +--> Windows input injection

&#x20;   |

&#x20;   +--> H.264 encoding using Windows Media Foundation

&#x20;   |

&#x20;   +--> Pion WebRTC

&#x20;            |

&#x20;            +--> video RTP

&#x20;            +--> reliable control DataChannel

&#x20;            +--> low-latency input DataChannel



The actual screen/media traffic MUST NOT go through Nostr.



Nostr is for:

\- WebRTC offer/answer

\- ICE candidate exchange

\- session discovery/signaling

\- authenticated session setup messages where appropriate



WebRTC handles:

\- live screen video

\- mouse input

\- scrolling

\- control/session messages



Direct LAN connectivity should naturally be preferred by ICE.



Remote connections should use peer-to-peer WebRTC when possible.



An optional TURN configuration may exist as a fallback, but PC Remote must NOT operate its own mandatory TURN service.



Public/default Nostr relays may remain the signaling fallback, consistent with the existing project architecture.



==================================================

CRITICAL WINDOWS ARCHITECTURE REQUIREMENT

==================================================



The existing PC Remote service runs as LocalSystem.



DO NOT move the service to another account merely to make Remote Desktop work.



A Windows service running in Session 0 cannot simply capture the interactive user's desktop.



Therefore:



The LocalSystem service remains the privileged background broker/orchestrator.



When a Remote Desktop session is requested, the service must start a temporary interactive-session worker using the authorized user's token.



Prefer using the EXISTING executable with a dedicated mode rather than introducing another permanently-installed executable.



For example:



PC-Remote.exe service

PC-Remote.exe tray

PC-Remote.exe session-agent



Do not turn the tray process into the screen-capture worker.



The session-agent:

\- runs inside the active interactive user session

\- exists only while Remote Desktop is active

\- owns screen capture

\- owns the WebRTC PeerConnection

\- owns Windows mouse/input interaction

\- owns the encoder

\- exits when the session ends



The service:

\- authenticates the paired phone

\- validates the session request

\- launches the session-agent securely

\- brokers/forwards signaling between the agent and the browser

\- owns the authoritative session lifecycle

\- terminates the worker when needed

\- remains responsible for security and policy



DO NOT send high-bandwidth screen frames through the existing named-pipe IPC.



The internal service <-> worker IPC is only for:

\- session setup

\- authentication

\- signaling messages

\- lifecycle commands

\- health/heartbeat

\- session termination

\- errors/status



Video remains on WebRTC directly from the session-agent to the browser.



==================================================

REMOTE SESSION SECURITY MODEL

==================================================



Remote Desktop is a higher-impact capability than Lock/Sleep/Restart/Shutdown.



Do NOT authorize it solely because the browser knows the device name or because the page is open.



Do NOT reintroduce a plaintext password-based remote-session protocol.



Use the existing cryptographic paired-phone identity.



The paired phone already has:

\- phone private key

\- phone public key

\- laptop public key



Use that existing identity model.



A Remote Desktop session should work approximately like:



1\. User is already paired.

2\. User opens Remote Desktop from the dashboard.

3\. UI shows an explicit consent/confirmation step.

4\. User taps "Allow Access" / equivalent.

5\. Browser creates a cryptographically authenticated session request.

6\. Service verifies:

&#x20;  - paired phone public key

&#x20;  - event signature

&#x20;  - intended laptop public key

&#x20;  - freshness/timestamp

&#x20;  - unique session ID

&#x20;  - replay resistance

7\. Service creates a short-lived session capability.

8\. Service launches session-agent for the currently authorized interactive user.

9\. WebRTC signaling occurs.

10\. WebRTC connection becomes established.

11\. The session-agent authenticates the browser identity over the established secure channel before exposing real screen frames or accepting input.

12\. Only after successful authentication:

&#x20;   - actual screen capture begins

&#x20;   - real video frames are sent

&#x20;   - mouse/input handling becomes active



Before authentication:

\- never transmit the actual desktop

\- never accept real mouse/keyboard/power-control input

\- a black/placeholder video frame is acceptable



The WebRTC DTLS connection is encrypted, but DO NOT assume encryption alone proves which paired phone is connected.



Bind the WebRTC session to the already-paired phone identity.



Use a challenge/response or equivalent cryptographic session-auth mechanism using the existing key architecture.



The session ID must be unpredictable.



Use cryptographically secure randomness.

Do NOT use Math.random, timestamps alone, predictable counters, UUID-v4 implementations with weak entropy, etc. for security tokens.



Every session must have:

\- unique random session ID

\- creation time

\- authenticated phone public key

\- explicit lifecycle state

\- timeout/heartbeat state



Prevent replay.



Reject:

\- stale signaling events

\- duplicated session IDs

\- malformed session messages

\- wrong laptop public key

\- wrong phone public key

\- unauthenticated input

\- input received after session termination

\- input before authentication



Never log:

\- private keys

\- decrypted private-key material

\- PIN values

\- full session secrets

\- raw authentication secrets



Logging may include:

\- session ID

\- lifecycle state

\- candidate type

\- connection state

\- sanitized error category



==================================================

SESSION CONCURRENCY

==================================================



For the first implementation, support ONE active Remote Desktop session per PC.



If a second session is requested:

\- reject it cleanly

\- do not spawn another encoder

\- do not create another capture worker

\- do not multiply CPU/GPU usage



Do not silently replace the active session.



This significantly reduces resource consumption and attack surface.



==================================================

SESSION LIFECYCLE

==================================================



Implement explicit states, for example:



Idle

Requesting

Launching

Signaling

Connecting

Authenticating

Active

Stopping

Stopped

Failed



Do not scatter boolean flags throughout the code.



Use a single clear session state machine where practical.



A session-agent must stop when:

\- user explicitly exits Remote Desktop

\- browser disconnects and does not recover within a short grace period

\- authentication fails

\- heartbeat/session timeout occurs

\- service terminates the session

\- worker encounters unrecoverable capture/encoder/WebRTC failure



Use a reasonable heartbeat timeout, e.g. approximately 10–15 seconds, but implement it as a constant/configurable value rather than scattering magic numbers.



When the session ends:

\- stop WebRTC

\- stop video capture

\- stop encoder

\- release D3D11 resources

\- release DXGI duplication

\- stop input processing

\- close IPC

\- terminate worker cleanly

\- ensure no background capture goroutine remains



The service must be capable of cleaning up a crashed/stuck session-agent.



The browser must not be able to leave screen capture running indefinitely simply by disappearing.



==================================================

SCREEN CAPTURE

==================================================



Use Windows Desktop Duplication API through DXGI/D3D11.



Do NOT use:

\- repeated screenshot APIs

\- GDI screen capture for every frame

\- PrintWindow

\- clipboard screenshots

\- browser polling

\- full CPU readback per frame

\- ffmpeg command-line screen capture



The implementation must avoid unnecessary GPU -> CPU -> GPU transfers.



Preferred pipeline:



Desktop Duplication

&#x20;   ->

D3D11 texture

&#x20;   ->

GPU-side conversion when possible

&#x20;   ->

Media Foundation H.264 encoder

&#x20;   ->

WebRTC H.264 RTP



Prefer GPU-backed processing.



Do not read full-screen BGRA frames into Go memory on every frame unless there is no viable alternative.



==================================================

DISPLAY SCOPE

==================================================



First version:

\- capture the PRIMARY display only



Do NOT implement multi-monitor switching yet.



But structure the code so multi-monitor support can be added later.



Create a clean display/capture abstraction rather than hardcoding assumptions throughout the code.



Use the actual display dimensions returned by Windows.



Correctly handle:

\- 1920x1080

\- 1366x768

\- 2560x1440

\- other common resolutions

\- display changes

\- resolution changes

\- monitor sleep/wake where practical



If the duplication device becomes invalid or Windows returns an access-lost condition:

\- release resources

\- recreate the duplication device/output

\- continue if possible

\- fail cleanly if recovery is impossible



Do not crash the service because screen capture failed.



The session-agent may terminate and the service remains alive.



==================================================

FRAME EFFICIENCY

==================================================



Do NOT blindly encode the desktop at maximum FPS forever.



Use Desktop Duplication information intelligently.



Prefer:

\- dirty-rectangle awareness

\- frame-change detection

\- skipping identical frames

\- pointer-position updates without forcing full-screen video encode where practical

\- frame pacing

\- bounded queues



When nothing changed on screen:

\- do not repeatedly encode identical frames



Mouse cursor movement should ideally be represented separately from the desktop image when practical.



Use Desktop Duplication pointer information where practical:

\- cursor position

\- visibility

\- cursor shape

\- hotspot



The browser may render a cursor overlay to avoid encoding the cursor into every frame.



However:

Do NOT turn cursor rendering into a complex independent graphics engine during the first implementation.



A clean, correct implementation is more important than micro-optimizing cursor rendering.



==================================================

VIDEO ENCODING

==================================================



Use H.264 because browser WebRTC compatibility and hardware decoding are important.



Prefer Windows Media Foundation rather than adding a huge external FFmpeg runtime.



Do not execute ffmpeg.exe as a background child process.



Do not introduce unnecessary heavyweight dependencies.



Prefer:

\- Media Foundation H.264 encoder MFT

\- hardware acceleration where available through Windows

\- Microsoft/software H.264 MFT fallback where appropriate



Keep the encoder behind a clean abstraction, e.g.:



ScreenEncoder



with operations equivalent to:

\- Initialize

\- EncodeFrame

\- RequestKeyFrame

\- SetBitrate

\- SetFrameRate

\- Close



Do not hardwire the capture implementation directly into WebRTC.



The encoder should produce a format that the chosen Pion/WebRTC H.264 packetization path can consume correctly.



Be careful about:

\- Annex-B vs AVCC formatting

\- SPS/PPS handling

\- keyframes

\- packetization

\- timestamps

\- RTCP feedback

\- frame boundaries



Test the output in an actual Chromium-based browser.



Do not claim it works merely because the Go code compiles.



==================================================

VIDEO DEFAULTS

==================================================



Optimize first for low-to-mid-range PCs rather than blindly targeting 4K/60.



Recommended starting target:



\- up to 1920x1080

\- default around 1280x720 when bandwidth/performance requires it

\- target \~30 FPS initially

\- sensible H.264 bitrate

\- adaptive/degradation hooks



Do not encode 1080p60 by default on a low-end machine.



Make capture/encoder settings centralized constants/config rather than magic numbers spread across files.



Prefer a design that can later adapt:

\- FPS

\- bitrate

\- resolution



based on network/CPU conditions.



Do not build a giant custom congestion-control algorithm in this first implementation.



Create clean extension points for future adaptation.



==================================================

WEBRTC

==================================================



Use a mature Go WebRTC implementation compatible with the current Go version.



Pion WebRTC is the preferred direction unless inspection of the current dependency tree reveals a compelling reason otherwise.



Do not invent a proprietary RTP/WebRTC implementation.



WebRTC should handle:

\- DTLS

\- ICE

\- SRTP

\- RTP

\- data channels



Use separate logical data channels:



1\. Low-latency INPUT channel

&#x20;  - unordered

&#x20;  - no retransmission / limited retransmission

&#x20;  - pointer movement

&#x20;  - high-frequency scroll



2\. CONTROL channel

&#x20;  - ordered

&#x20;  - reliable

&#x20;  - clicks

&#x20;  - session lifecycle

&#x20;  - quick actions

&#x20;  - other discrete control messages



Do not send 100 pointer events into a reliable queue and allow them to backlog.



Coalesce pointer movement.



Prefer a frame-paced input sender, roughly around display/input frame rate rather than unlimited event spam.



If the browser produces 300 touch events per second:

\- do NOT forward all 300

\- keep only the latest necessary state

\- preserve discrete click events



==================================================

WEBRTC SIGNALING

==================================================



Do not create a new central signaling server.



Use the existing Nostr infrastructure.



Signaling messages must be:

\- authenticated

\- encrypted where appropriate

\- scoped to the target laptop public key

\- bound to the session ID

\- freshness-checked

\- replay-resistant



Support messages equivalent to:



offer

answer

ice-candidate

session-auth

session-accepted

session-rejected

session-close

error



Use a clean typed protocol instead of arbitrary JSON blobs scattered through the code.



If the existing project already has a reusable encrypted Nostr envelope abstraction:

REUSE IT.



Do not create a second incompatible crypto protocol.



Do not duplicate existing NIP-44 implementation unnecessarily.



==================================================

ICE / CONNECTIVITY

==================================================



Target connection strategy:



1\. Direct LAN / host candidate when same network.

2\. Direct peer-to-peer WebRTC through NAT traversal where possible.

3\. Optional TURN fallback if user configures one.



Do NOT make TURN mandatory.



Do NOT proxy screen video through Nostr.



Do NOT proxy video through Cloudflare Pages.



Do NOT proxy video through a custom server.



Make STUN/TURN configuration centralized and replaceable.



Public/default STUN may be used if necessary for WebRTC NAT traversal, but:

\- it must never carry the screen stream

\- it must be replaceable by user configuration

\- the project must not depend on a paid vendor infrastructure



Use a sensible ICE policy.



Avoid exposing unnecessary network listeners.



Where practical, bind WebRTC UDP to a predictable port that can be handled by the existing firewall architecture.



Because the existing HTTP service already uses TCP port 8765, using UDP 8765 for WebRTC may be considered, since TCP and UDP are separate transports.



If that approach is used:

\- update installer firewall configuration appropriately

\- private network profile only

\- document why

\- do not expose unnecessary public firewall rules



Do not open a broad UDP range unless technically necessary.



==================================================

INPUT SYSTEM

==================================================



Implement a clear input protocol.



Example conceptual messages:



{

&#x20; "type": "mouse\_move",

&#x20; "mode": "relative",

&#x20; "dx": 12,

&#x20; "dy": -4

}



{

&#x20; "type": "mouse\_move",

&#x20; "mode": "absolute",

&#x20; "x": 854,

&#x20; "y": 412

}



{

&#x20; "type": "mouse\_click",

&#x20; "button": "left"

}



{

&#x20; "type": "mouse\_click",

&#x20; "button": "right"

}



{

&#x20; "type": "mouse\_click",

&#x20; "button": "middle"

}



{

&#x20; "type": "scroll",

&#x20; "dx": 0,

&#x20; "dy": -5

}



Do not blindly trust any client-provided values.



Validate:

\- message type

\- required fields

\- numeric ranges

\- NaN/infinite values

\- coordinate bounds

\- scroll magnitude

\- message size

\- maximum rate



Reject malformed input.



Never allow the protocol to evolve into:

\- shell commands

\- arbitrary process execution

\- arbitrary PowerShell

\- arbitrary file access



Remote Desktop input is explicit mouse/keyboard control only.



==================================================

WINDOWS INPUT

==================================================



Use safe Windows input APIs appropriate for an interactive user session.



Prefer SendInput / appropriate Win32 input mechanisms.



Support:

\- absolute pointer movement

\- relative pointer movement

\- left click

\- right click

\- middle click

\- vertical/horizontal scrolling where practical



Handle Windows coordinate systems correctly.



Do not assume the screen is always 1920x1080.



Respect:

\- primary display dimensions

\- virtual-screen coordinates

\- display origin

\- DPI/scaling implications



Do not create an unsafe UAC bypass.



Do not attempt undocumented tricks to defeat secure desktop/UAC security boundaries.



If elevated/UAC applications cannot accept input under the current process integrity level:

\- fail safely

\- document the limitation

\- do not weaken Windows security to bypass it



Structure this so stronger UIAccess support can potentially be added later with proper signing/manifest architecture.



==================================================

GESTURE MODEL — PORTRAIT

==================================================



Portrait mode has TWO separate regions:



1\. Screen preview at the top

2\. Touch-control area below



The screen preview is display-only in portrait.



Do NOT accidentally turn portrait screen preview into a touch input surface.



Touch control area behaves like a trackpad.



Required behavior:



ONE FINGER:

\- drag -> relative mouse movement

\- tap -> left click



LONG PRESS:

\- -> right click



TWO-FINGER TAP:

\- -> middle click



TWO-FINGER DRAG:

\- -> scrolling



Use a proper gesture state machine.



Do not produce accidental clicks while interpreting movement.



Use configurable thresholds/constants for:

\- tap duration

\- long-press duration

\- movement tolerance

\- gesture activation

\- pointer sensitivity

\- scroll sensitivity



Do not scatter magic numbers through app.js.



A tap after large movement should not become a click.



A long press should not generate both left click and right click.



Two-finger gestures should not duplicate single-finger actions.



Keep gesture recognition client-side for responsiveness.



Send compact semantic input events rather than every raw browser touch event.



==================================================

GESTURE MODEL — LANDSCAPE

==================================================



Landscape mode is fundamentally different.



The streamed PC screen itself is the control surface.



No separate touchpad.



No header.



The PC display should occupy essentially the entire phone viewport.



Touch over the visible screen controls the corresponding PC screen coordinates.



Use absolute-coordinate mapping.



Correctly handle letterboxing/aspect-ratio differences.



Example:



PC capture:

1920x1080



Phone landscape viewport:

2340x1080



The visible video may have horizontal/vertical fitting.



Input coordinates MUST be transformed based on the actual displayed video rectangle.



Do not assume:

videoWidth == CSS width

videoHeight == CSS height



Take:

\- rendered element bounds

\- actual video dimensions

\- object-fit behavior

\- letterboxing/pillarboxing



into account.



Ignore touches outside the actual video content when letterboxed.



Required behavior:



\- tap -> left click

\- long press -> right click

\- two-finger tap -> middle click

\- two-finger drag -> scroll

\- drag -> move pointer according to screen coordinates



Use local visual feedback only where it improves usability.



Do not add distracting cursor animations.



==================================================

REMOTE DESKTOP UI

==================================================



Add a Remote Desktop entry/button somewhere appropriate on the existing dashboard.



Use the existing design language.



Do not redesign the entire dashboard.



Suggested flow:



Dashboard

&#x20;  ->

Remote Desktop

&#x20;  ->

Consent / access sheet

&#x20;  ->

"Allow Access"

&#x20;  ->

Remote Desktop page



The consent UI should clearly state that this will:

\- share the PC screen with the paired device

\- allow mouse/input control



Do not ask for the Master PIN again if the phone is already authenticated/unlocked.



The paired cryptographic identity remains the underlying authorization mechanism.



==================================================

PORTRAIT UI

==================================================



Portrait layout should approximately be:



\------------------------------------------------

HEADER

\------------------------------------------------

network status      battery

\------------------------------------------------

&#x20;               PC SCREEN

\------------------------------------------------

&#x20;               TOUCH AREA

\------------------------------------------------

&#x20;            floating power

&#x20;            controls button

\------------------------------------------------



Header:

\- actual transport state

\- battery percentage

\- charging state where available



Use the existing transport semantics:



● LOCAL

when the actual WebRTC connection is direct LAN/local



● CONNECTED

when remote/WebRTC is otherwise connected



DO NOT infer transport from config.lanHost.



Use the actual connection path.



Do not show CPU/memory/uptime cards here.



Keep the header compact.



Remote screen should take the upper portion of the viewport.



Control area should be large enough for comfortable thumb use.



Do not make the UI look like a generic enterprise dashboard.



Keep it modern, dark, clean, compact, and touch-friendly.



==================================================

PORTRAIT QUICK ACTIONS

==================================================



Floating button:

\- bottom center

\- visually small but easy to tap

\- translucent/modern

\- should not obstruct the touch area excessively



When tapped:

show quick actions for:



\- Lock

\- Sleep

\- Restart

\- Shutdown



Reuse the EXISTING command/action pipeline.



Do NOT duplicate power-action implementation inside Remote Desktop.



Existing confirmations for destructive actions must remain.



Do not bypass existing authorization.



The quick-action menu should close when:

\- an action is selected

\- user taps outside

\- user dismisses it



==================================================

LANDSCAPE UI

==================================================



Landscape:

\- no header

\- no unnecessary browser-like chrome inside the application

\- screen fills viewport

\- screen itself is the input surface



Add a small floating quick-action button in one corner.



It should open the same Lock/Sleep/Restart/Shutdown controls.



Keep it unobtrusive.



Add a small landscape/orientation control in portrait mode.



Use the browser's supported Screen Orientation API where available.



Do not assume orientation locking always succeeds on every browser.



Handle failure gracefully.



Do not make the application unusable just because orientation locking is unavailable.



==================================================

NAVIGATION / EXIT

==================================================



Remote Desktop is a separate application state/page.



Use the existing navigation/state architecture rather than introducing a second routing system.



When the user leaves Remote Desktop:

\- terminate the session

\- stop local WebRTC

\- notify the PC where possible

\- release browser resources

\- return to dashboard cleanly



Also handle:

\- browser back button

\- pagehide

\- visibility changes

\- tab suspension where applicable

\- WebRTC connection failure



Do not terminate and restart the session merely because visibility temporarily changes unless necessary.



The PC-side heartbeat/session timeout is the final cleanup safety net.



==================================================

BROWSER PERFORMANCE

==================================================



The web client must be efficient.



Do NOT:

\- create unnecessary DOM nodes per frame

\- redraw the entire UI for every video frame

\- use canvas to manually copy the entire video on every frame

\- encode/decode video in JavaScript

\- poll the screen continuously

\- send raw touchmove events without throttling/coalescing

\- allocate large objects repeatedly in high-frequency paths



Use:

\- HTML video element for WebRTC video

\- lightweight overlay layers

\- Pointer/Touch Events appropriately

\- requestAnimationFrame or equivalent pacing where needed

\- bounded input queues

\- event coalescing



Do not run expensive React-like state rerenders if the existing app is vanilla JS.



Follow the existing web architecture.



==================================================

WEBRTC VIDEO RENDERING

==================================================



Prefer:



<video autoplay playsinline>



with WebRTC MediaStream.



Do not copy every video frame into canvas.



Canvas should only be considered for special overlays if genuinely required.



The video element must:

\- preserve aspect ratio

\- support portrait preview mode

\- support full-screen landscape

\- resize correctly

\- survive orientation changes

\- recover after reconnection where practical



==================================================

CURSOR HANDLING

==================================================



Desktop Duplication does not necessarily contain the final mouse cursor image in the normal desktop texture.



Prefer a lightweight cursor-overlay architecture:



PC:

\- pointer position

\- visibility

\- cursor-shape updates when changed



Browser:

\- render cursor overlay above video



Do not send cursor shape data on every frame.



Only send updates when:

\- cursor moves

\- cursor becomes visible/hidden

\- cursor shape changes



Put strict limits on cursor bitmap sizes and message size.



If implementing cursor-shape transport adds excessive complexity:

\- use a clean generic cursor overlay for v1

\- keep the protocol extensible

\- do not destabilize video streaming



==================================================

NETWORK / TRANSPORT DISPLAY

==================================================



The Remote Desktop page should reflect actual connection state.



States should be explicit:



Connecting

Authenticating

Connected

Disconnected

Failed



For the network label:

LOCAL = actual direct local connection

CONNECTED = actual remote/P2P connection



Do not infer LOCAL merely because lanHost exists.



Determine actual selected WebRTC candidate pair/path where possible.



Do not lie to the user about transport.



==================================================

RESOURCE LIMITS

==================================================



The user's machine can be relatively low-end.



Design for low idle and active overhead.



Only create expensive resources after a Remote Desktop session is actually authorized.



When idle:

\- no screen capture

\- no encoder

\- no D3D duplication surface

\- no persistent high-frequency capture loop

\- no remote input processing loop



During session:

\- bounded memory

\- bounded queues

\- no unbounded channel growth

\- backpressure where appropriate

\- avoid copying full frames unnecessarily



Only one active Remote Desktop session.



On session shutdown:

immediately release resources.



Do NOT allow a dropped browser tab to keep:

\- D3D resources

\- encoder

\- WebRTC peer

\- goroutines

alive indefinitely.



==================================================

ERROR HANDLING

==================================================



Every major layer must fail gracefully.



Capture failure:

\-> terminate session cleanly

\-> service remains healthy



Encoder failure:

\-> terminate/reinitialize session if recovery is practical

\-> never crash service



WebRTC failure:

\-> transition session state

\-> cleanup worker



Signaling timeout:

\-> fail session cleanly



Authentication failure:

\-> reject

\-> no screen exposure

\-> no input



Browser disconnect:

\-> heartbeat timeout cleanup



Session-agent crash:

\-> service detects it

\-> closes session

\-> service remains alive



Do not use panic for normal runtime failures.



==================================================

CODE ORGANIZATION

==================================================



Do not dump everything into one giant file.



Create focused packages/files.



Suggested conceptual organization:



internal/remote/

&#x20;   session.go

&#x20;   protocol.go

&#x20;   auth.go

&#x20;   signaling.go

&#x20;   manager.go



internal/remote/capture/

&#x20;   capture\_windows.go

&#x20;   desktop\_duplication\_windows.go



internal/remote/encoder/

&#x20;   encoder.go

&#x20;   mediafoundation\_windows.go



internal/remote/webrtc/

&#x20;   peer.go

&#x20;   signaling.go

&#x20;   input.go



internal/remote/input/

&#x20;   input\_windows.go

&#x20;   coordinates\_windows.go



internal/remote/worker/

&#x20;   worker\_windows.go



Names are suggestions, not requirements.



Follow the existing repository conventions if there is a better structure already present.



Keep interfaces narrow.



Separate:

\- session orchestration

\- capture

\- encoding

\- networking

\- input

\- authentication

\- UI



Do not create circular dependencies.



Do not make the web UI aware of Go implementation details.



==================================================

CRYPTOGRAPHY / PROTOCOL REUSE

==================================================



Before implementing anything:

inspect the existing crypto implementation.



Reuse:

\- existing key format

\- existing NIP-44 implementation

\- existing Nostr event signing/verification

\- existing paired-device identity

\- existing device authorization logic



DO NOT create:

\- another PIN hash

\- another key store

\- another encryption format

\- another private-key transport

\- another "temporary password" system



The Remote Desktop feature must fit into the existing security model, not create a parallel security model.



==================================================

LOCAL IPC SECURITY

==================================================



If the service launches the session-agent, the service <-> worker IPC must be authenticated and restricted.



Do not create a world-writable localhost TCP socket for the worker.



Prefer a Windows named pipe with:

\- SYSTEM access

\- appropriate authorized-user SID access

\- strong pipe restrictions

\- explicit server-side client identity validation



Reuse the current hardened IPC approach where possible.



Avoid:

\- broad Everyone permissions

\- anonymous access

\- IU broad grants

\- unauthenticated localhost HTTP as a privileged control channel



Use a per-session pipe name or cryptographically unpredictable session-specific endpoint where practical.



Authenticate the session-agent before trusting signaling/control data.



==================================================

SERVICE / TRAY BOUNDARY

==================================================



The tray remains a lightweight UI process.



Do NOT:

\- move screen capture into tray

\- move WebRTC into tray

\- make tray responsible for session reliability

\- make tray a required component for Remote Desktop



Service must continue working when:

\- tray is closed

\- tray crashes

\- tray is not running

\- user is logged out



Remote Desktop requires an interactive user session because screen capture/input need one.



That should be handled through the session-agent.



==================================================

INSTALLER

==================================================



Inspect the existing installer before changing it.



Only change what is required.



Potential changes:

\- required UDP firewall rule for WebRTC

\- any manifest capability required by the implementation

\- service launch behavior if needed

\- permissions for installed files



Do NOT:

\- open broad public firewall rules

\- add unnecessary startup processes

\- install large external runtime packages

\- require users to install another VPN



The existing service/tray installation architecture must continue functioning.



==================================================

TESTING REQUIREMENTS

==================================================



Add tests for every nontrivial security/protocol component.



At minimum:



1\. Session ID generation

&#x20;  - unpredictable

&#x20;  - non-empty

&#x20;  - unique across many generated IDs



2\. Session expiration

&#x20;  - stale sessions rejected



3\. Replay protection

&#x20;  - duplicate signaling/session messages rejected



4\. Phone identity validation

&#x20;  - correct paired phone accepted

&#x20;  - wrong phone rejected



5\. Laptop identity validation

&#x20;  - wrong target rejected



6\. Input validation

&#x20;  - oversized values rejected

&#x20;  - NaN/infinite invalid values rejected

&#x20;  - coordinates outside valid range rejected

&#x20;  - invalid buttons rejected

&#x20;  - oversized messages rejected

&#x20;  - invalid message types rejected



7\. Gesture state machine

&#x20;  - tap => left

&#x20;  - long press => right

&#x20;  - two-finger tap => middle

&#x20;  - drag => movement

&#x20;  - two-finger drag => scroll

&#x20;  - movement must not accidentally trigger click



8\. Coordinate transforms

&#x20;  - 16:9 screen

&#x20;  - 4:3 screen

&#x20;  - letterboxed video

&#x20;  - portrait/landscape changes



9\. Session cleanup

&#x20;  - disconnect causes cleanup

&#x20;  - timeout causes cleanup

&#x20;  - authentication failure causes cleanup



10\. IPC authorization

&#x20;  - unauthorized local identity rejected

&#x20;  - authorized SID accepted

&#x20;  - SYSTEM accepted where intended

&#x20;  - existing admin/security semantics preserved



Add build-tagged Windows tests for Windows-specific functionality where appropriate.



Do not require a real monitor/GPU for pure unit tests.



Use abstractions/interfaces so capture and encoder logic can be tested independently.



==================================================

MANUAL WINDOWS TEST CHECKLIST

==================================================



After implementation, actually test on Windows.



At minimum:



PAIRING:

\- existing pairing still works



DASHBOARD:

\- Remote Desktop entry visible

\- existing controls still work



SESSION:

\- tap Remote Desktop

\- consent screen

\- allow

\- connection establishes



PORTRAIT:

\- header displays correctly

\- screen preview updates

\- trackpad responds

\- tap = left click

\- long press = right click

\- two-finger tap = middle click

\- two-finger drag = scroll

\- floating actions work



LANDSCAPE:

\- orientation transition works where browser allows

\- no header

\- PC screen fills display

\- touch location maps correctly

\- tap/click works

\- drag works

\- scrolling works

\- floating action button works



NETWORK:

\- same Wi-Fi

\- remote network/mobile data

\- connection loss

\- reconnection



LIFECYCLE:

\- user exits page

\- browser tab closes

\- browser refreshes

\- phone locks screen temporarily

\- laptop locks

\- laptop wakes

\- display resolution changes if possible



SECURITY:

\- unpaired phone cannot create session

\- wrong paired key cannot authenticate

\- stale session cannot authenticate

\- replay cannot authenticate

\- no screen is sent before authentication

\- input rejected before authentication



WINDOWS:

\- service remains alive if session-agent crashes

\- service remains alive if WebRTC fails

\- tray can exit without killing service

\- reboot/service restart does not break existing core features

\- session-agent exits after session ends



==================================================

PERFORMANCE MEASUREMENT

==================================================



Do not say "optimized" without measuring.



Measure at minimum:



IDLE:

\- service private working set

\- CPU

\- tray private working set

\- CPU



REMOTE DESKTOP ACTIVE:

\- service RAM

\- session-agent RAM

\- total PC Remote CPU

\- GPU utilization if measurable

\- encode FPS

\- delivered FPS

\- capture FPS

\- WebRTC bitrate

\- dropped frames

\- connection setup time



Test:

1\. 720p class display

2\. 1080p display if available



Do not optimize only based on code appearance.



Avoid premature complexity that provides no measured benefit.



The current PC Remote service is intentionally lightweight.

Remote Desktop is allowed to consume additional resources while ACTIVE,

but those resources must disappear again after the session ends.



==================================================

DEPENDENCY POLICY

==================================================



Keep the project lightweight.



Before adding dependencies:

\- inspect whether existing code already solves the problem

\- prefer maintained mature libraries

\- avoid duplicate libraries solving the same thing

\- avoid giant multimedia stacks unless absolutely necessary



Do not add Electron.



Do not add Chromium.



Do not add FFmpeg runtime processes.



Do not add a local database.



Do not add a cloud SDK.



Do not add a proprietary remote desktop SDK.



Pion WebRTC is acceptable.



Windows system APIs / Media Foundation / DXGI / D3D11 are preferred for native functionality.



==================================================

UI CODE QUALITY

==================================================



Inspect existing web UI conventions first.



Do not rewrite the application's existing visual language.



The Remote Desktop UI should feel like it belongs inside PC Remote.



Use:

\- existing CSS variables

\- existing typography

\- existing buttons/icons where appropriate

\- existing modal/notification patterns



Do not introduce random icon libraries solely for this feature if the project already has an icon strategy.



No unnecessary animations.



No giant shadows or "cartoon" styling.



Keep interaction responsive on mobile browsers.



Avoid layout jumps during connection.



Do not flash internal screens before state evaluation.



Respect the application's existing public/unpaired landing behavior.



==================================================

ACCESSIBILITY / TOUCH

==================================================



Touch targets must be large enough for phone use.



Prevent accidental browser gestures where appropriate inside the remote-control surface, especially:

\- pull-to-refresh

\- text selection

\- context menu

\- browser double-tap zoom where it interferes

\- page scrolling inside control area



Do not disable browser behavior globally.



Apply touch handling only to the appropriate Remote Desktop surfaces.



Use `touch-action` correctly.



==================================================

DO NOT DO THESE THINGS

==================================================



DO NOT:



\- rewrite the whole application

\- replace Nostr

\- replace the existing pairing architecture

\- create a central backend

\- route video through a server

\- route video through Nostr

\- send screenshots as JSON/base64

\- poll screenshots over HTTP

\- use GDI screenshot polling

\- spawn ffmpeg.exe

\- make tray responsible for screen capture

\- use LocalStorage as the authoritative secret store

\- add another password system

\- trust browser-supplied session IDs without authentication

\- accept unauthenticated mouse commands

\- expose an unauthenticated public WebRTC server

\- create a broad localhost TCP control port

\- create an unauthenticated WebSocket

\- accept arbitrary commands

\- add shell execution capability

\- add file-system access

\- add arbitrary PowerShell execution

\- weaken Windows security to control UAC/secure desktop

\- support multiple simultaneous sessions in v1

\- implement multiple monitors in v1

\- over-engineer adaptive streaming before basic streaming works

\- add dozens of configuration options to the first version

\- make the app depend on the tray

\- leave capture running after a dropped session

\- leave goroutines/channels/resources alive after session shutdown

\- claim functionality is complete without testing it



==================================================

IMPLEMENTATION PROCESS

==================================================



Work in phases.



PHASE 1

Inspect the repository and produce a short implementation map:

\- existing relevant modules

\- crypto reuse points

\- Nostr reuse points

\- service lifecycle

\- IPC reuse points

\- dashboard UI insertion point

\- installer changes

\- proposed files/packages



Do not modify code until this inspection is complete.



PHASE 2

Implement the Remote Desktop protocol/session state machine without full capture.



PHASE 3

Implement the interactive session-agent architecture.



PHASE 4

Implement Desktop Duplication capture.



PHASE 5

Implement Media Foundation H.264 encoding.



PHASE 6

Implement Pion WebRTC video transport.



PHASE 7

Implement authenticated control DataChannels.



PHASE 8

Implement browser Remote Desktop UI.



PHASE 9

Implement portrait gesture controls.



PHASE 10

Implement landscape screen interaction.



PHASE 11

Implement quick actions using existing command pipeline.



PHASE 12

Implement cleanup/recovery/error handling.



PHASE 13

Update tests.



PHASE 14

Update installer/documentation only where required.



PHASE 15

Run real Windows testing and measure performance.



Do not declare success if a phase is merely stubbed.



==================================================

IMPORTANT ARCHITECTURAL QUALITY RULE

==================================================



Prefer a small number of well-defined components over a huge abstraction framework.



The code should remain understandable by a future maintainer.



Avoid:

\- 1,000-line "god files"

\- giant switch statements handling every subsystem

\- duplicated crypto

\- duplicated command dispatch

\- duplicated session state

\- global mutable state where avoidable

\- goroutine leaks

\- channels with no ownership/lifecycle model

\- unclear shutdown behavior



Every long-lived goroutine must have:

\- an owner

\- a cancellation mechanism

\- a clear shutdown path



Every resource must have:

\- acquisition point

\- ownership

\- release path



Every network message must have:

\- type

\- validation

\- lifecycle

\- authentication expectations



==================================================

FINAL DELIVERABLE

==================================================



At the end:



1\. Build the project successfully.

2\. Run all existing tests.

3\. Run all new tests.

4\. Fix regressions.

5\. Verify the existing dashboard controls still work.

6\. Verify pairing still works.

7\. Verify LAN control still works.

8\. Verify remote signaling still works.

9\. Verify Remote Desktop works on an actual Windows machine.

10\. Measure resource usage.

11\. Review the code specifically for:

&#x20;   - security

&#x20;   - memory leaks

&#x20;   - goroutine leaks

&#x20;   - unbounded queues

&#x20;   - race conditions

&#x20;   - authentication bypasses

&#x20;   - replay attacks

&#x20;   - stale sessions

&#x20;   - screen leakage after disconnect

&#x20;   - input injection before authentication

&#x20;   - improper cleanup

&#x20;   - accidental public listeners

&#x20;   - unsafe logging



Then give a concise implementation report containing:



\- files changed

\- major architecture added

\- dependencies added

\- security model

\- session lifecycle

\- WebRTC/signaling design

\- capture/encoding design

\- UI behavior

\- tests added

\- Windows tests performed

\- measured RAM/CPU/GPU/bitrate/FPS where available

\- known limitations

\- anything that remains intentionally deferred



Most importantly:



DO NOT hide incomplete functionality behind claims like "implemented".

Clearly distinguish:

\- fully implemented

\- partially implemented

\- stubbed

\- tested

\- not tested



The quality bar is production-oriented:

secure, maintainable, resource-conscious, responsive, and coherent with the existing PC Remote architecture.

