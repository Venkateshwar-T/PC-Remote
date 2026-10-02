FIX THE CURRENT REMOTE DESKTOP "REQUEST TIMED OUT" BUG AND THE SIGNALING PROTOCOL MISMATCH.

I tested the latest main branch after the MsgAgentHello named-pipe fix.

Current behavior:

1. Click Remote Desktop.
2. Click Allow Access.
3. Remote Desktop view appears.
4. PC briefly shows a busy/spinning cursor.
5. The remote view remains black / does not become usable.
6. After waiting, browser returns to Dashboard with:
   "Failed to start remote desktop: Request timed out"

I inspected the current codebase.

The named-pipe handshake issue has already been fixed.
Do NOT undo that fix.

ROOT CAUSE #1:
The Remote Desktop signaling request is asynchronous on the host, but the browser treats it as a normal request/response RPC.

Current browser flow:

sendRemoteSignal()
    -> sendRequest('remote_signal', ...)
    -> waits for matching request ID
    -> timeout after 6500 ms

Current protocol handler:

case "remote_signal":
    ...
    replySig, err := rm.HandleSignaling(...)
    ...
    if replySig != nil {
        return encrypted response...
    }
    return nil, nil // Asynchronous signaling will be emitted by worker

Therefore the original request never receives a correlated response.

The worker's answer is emitted asynchronously through the remote manager's outbound signaling callback, but that does NOT resolve the original pending request.

This MUST be fixed.

==================================================
REQUIRED ARCHITECTURE
==================================================

Keep asynchronous WebRTC signaling.

Do NOT force all signaling through a synchronous request/response model.

The correct distinction is:

REQUEST/RESPONSE:
- remote_request
- normal PC commands

ASYNC SIGNALING:
- offer
- answer
- ICE candidates
- session close
- signaling state

The browser must NOT wait 6.5 seconds for an async signaling request to complete merely because the signaling message itself was accepted.

==================================================
PREFERRED FIX
==================================================

When the host successfully accepts/forwards a remote_signal message:

return an IMMEDIATE correlated acknowledgment using the SAME request ID.

For example:

{
    id: <original request ID>,
    status: "ok",
    message: "signaling forwarded"
}

The actual WebRTC answer/candidates remain asynchronous signaling messages.

Therefore:

remote_signal request
    ->
host validates
    ->
host forwards to session-agent
    ->
host immediately returns ACK
    ->
browser resolves sendRemoteSignal()
    ->
actual answer arrives separately through the existing async signaling path

Do NOT block the HTTP/Nostr command request waiting for the WebRTC worker.

Do NOT invent arbitrary fake timing.

Do NOT increase the 6500 ms timeout as a workaround.

==================================================
PROTOCOL HANDLER CHANGE
==================================================

Inspect:

internal/protocol/handler.go

Current remote_signal branch:

    replySig, err := rm.HandleSignaling(evt.PubKey, *cmd.Signal)

If forwarding succeeds and replySig == nil, return a normal encrypted response packet:

ResponsePacket{
    ID:        cmd.ID,
    Status:    "ok",
    Message:   "signaling forwarded",
    Timestamp: time.Now().Unix(),
}

This MUST be returned for successfully forwarded asynchronous signaling.

If HandleSignaling returns an actual immediate reply signal:
preserve that behavior and include Signal as before.

If HandleSignaling fails:
return the existing error response.

Do not return nil,nil for a successfully forwarded command.

This ensures the browser's pendingRequests entry resolves correctly.

==================================================
VERY IMPORTANT: FIX SIGNAL TYPE MISMATCH
==================================================

The Go canonical values in:

internal/remote/session/types.go

are:

offer
answer
ice-candidate
session-auth
session-challenge
session-accepted
session-rejected
session-close
heartbeat
error

The browser currently sends/handles:

candidate
close

This is inconsistent and must be corrected.

Update the web client to use the exact canonical names.

Browser -> host:
- offer
- ice-candidate
- session-close
- heartbeat

Host -> browser:
- answer
- ice-candidate
- session-close
- error
etc.

Do NOT modify the Go protocol merely to accommodate the incorrect browser strings.

Use the existing Go protocol as the canonical protocol definition.

==================================================
WEB CLIENT CHANGES
==================================================

Inspect:

web/app.js

Current code sends:

type: 'candidate'

Change this to:

type: 'ice-candidate'

Current code sends:

type: 'close'

Change this to:

type: 'session-close'

Current incoming handler currently checks:

signal.type === 'candidate'

Change to:

signal.type === 'ice-candidate'

Current incoming handler currently checks:

signal.type === 'close'

Change to:

signal.type === 'session-close'

Do not add duplicate aliases unless necessary for backwards compatibility with an already deployed version.

The current protocol is still under active development, so use one canonical naming scheme.

==================================================
sendRemoteSignal() BEHAVIOR
==================================================

After the host starts returning the immediate ACK, update the browser code so that:

async function sendRemoteSignal(sig)

1. Sends remote_signal.
2. Waits only for the immediate ACK.
3. If ACK contains an actual response signal, process it.
4. Otherwise simply return the ACK.

Do NOT treat "no signal in ACK" as an error.

The actual answer/ICE packets will arrive through the existing asynchronous remote signaling path.

Example conceptual behavior:

const res = await sendRequest('remote_signal', { signal: sig });

if (res && res.signal) {
    await handleIncomingRemoteSignal(res.signal);
}

return res;

That part may already work once the host sends the ACK.

==================================================
ASYNC ANSWER PATH
==================================================

Preserve the current architecture where the worker sends:

MsgSignalAgentToPhone

and SessionManager.handleAgentMessage() calls:

outboundSignal(phonePubKey, packet)

and Daemon publishes the encrypted signaling response through Nostr.

Do not remove this asynchronous path.

Ensure that:

- offer answer arrives asynchronously
- browser processes it even though the original remote_signal ACK is separate
- answer is matched to the active session ID
- wrong-session packets are ignored
- wrong-laptop packets are ignored
- wrong-phone packets are ignored

The browser's async signaling handler must continue to work without depending on the original request ID.

==================================================
LOCAL LAN PATH
==================================================

Inspect:

internal/server/local_server.go

and the current handling of ProcessCommandEvent() returning nil.

Currently /api/control may encode a nil response.

After this fix, remote_signal should return a real encrypted response event containing the ACK.

Therefore direct LAN signaling must also return a valid encrypted response.

Do not leave the LAN path returning JSON null for a successfully forwarded remote_signal.

The same cryptographic response mechanism must be used as other commands.

==================================================
NOSTR PATH
==================================================

For remote/Nostr transport:

The original remote_signal request must receive an encrypted ACK with the ORIGINAL request ID.

The worker-generated async answer may remain a separate encrypted event.

The browser must process both.

Example:

EVENT A:
    request ID = req_123
    action = remote_signal
    signal = offer

HOST RESPONSE A:
    id = req_123
    status = ok
    message = signaling forwarded

EVENT B:
    id = sig_456
    signal = answer

Browser:
    resolves pending req_123 from A
    processes answer from B independently

Do NOT attempt to make EVENT B have the original request ID unless you redesign the entire signaling correlation system intentionally.

==================================================
SESSION CLOSE
==================================================

The current browser sends "close", which the Go validator rejects.

Correct it to "session-close".

The server already understands:

SignalSessionClose

and:

m.sm.Terminate(...)

Make sure it is actually reached.

After the browser exits Remote Desktop:
- session-close reaches host
- StateMachine terminates session
- session manager cleans pipe/process
- session-agent exits
- WebRTC closes
- capture closes
- encoder closes

No lingering worker should remain.

==================================================
IMPORTANT CLEANUP BUG TO REVIEW
==================================================

Inspect:

internal/remote/manager.go

Current HandleSessionRequest holds:

m.mu.Lock()
defer m.mu.Unlock()

for the entire function, including:
- spawning worker
- waiting for pipe Accept()
- waiting up to 5 seconds

This is unnecessarily broad locking.

Do NOT blindly rewrite it, but audit whether this lock can interact with:
- cleanupActiveSession()
- state machine termination callbacks
- async signaling
- worker disconnect callbacks

Avoid holding SessionManager's global mutex while performing slow blocking operations.

Refactor only if needed to prevent deadlocks/races.

Use narrow critical sections.

Do NOT change behavior unnecessarily.

==================================================
VERIFY WORKER STARTUP
==================================================

After the signaling fix, trace the full sequence:

Browser:
remote_request
    ↓
Host creates session
    ↓
session pipe created
    ↓
session-agent spawned
    ↓
agent hello
    ↓
Windows identity verified
    ↓
InitSession
    ↓
worker initializes
    ↓
browser sends offer
    ↓
host ACKs offer forwarding immediately
    ↓
worker receives offer
    ↓
worker creates answer
    ↓
host emits async answer
    ↓
browser receives answer
    ↓
ICE negotiation
    ↓
WebRTC connected
    ↓
DataChannels opened
    ↓
browser signs challenge
    ↓
worker verifies signature
    ↓
auth_success
    ↓
capture begins
    ↓
video appears

Do not skip steps.

==================================================
DO NOT HIDE THE NEXT FAILURE
==================================================

The current timeout is masking later stages.

After fixing the timeout, make logging detailed enough to determine exactly where the session gets stuck.

Use sanitized logs such as:

[RemoteMgr] Session X received offer
[RemoteMgr] Session X forwarded offer to agent
[RemoteMgr] Session X signaling ACK sent
[Agent] Session X received offer
[Agent] Session X generated answer
[WebRTC] Session X connection state: connecting
[WebRTC] Session X connection state: connected
[WebRTC] Session X in-band authentication PASSED
[Agent] Session X starting capture

Do NOT log:
- private keys
- auth secrets
- full SDP if it contains unnecessary sensitive/local network details
- session secrets

Session ID is acceptable.

==================================================
SECURITY
==================================================

Do NOT weaken any current security mechanism to fix this.

Preserve:
- cryptographic phone authorization
- NIP-44
- signed commands
- replay protection
- session ID validation
- session authentication
- Windows named-pipe identity verification
- authorized SID restrictions
- one-session limit
- heartbeat timeout

Do not use:
- unauthenticated WebSockets
- localhost TCP without authentication
- plain passwords
- client-supplied "trusted" flags

==================================================
TESTS
==================================================

Add regression tests for:

1. remote_signal successful forwarding returns immediate ACK

2. ACK preserves original request ID

3. ACK status is "ok"

4. worker-generated answer remains asynchronous

5. canonical "ice-candidate" accepted

6. "candidate" rejected

7. canonical "session-close" accepted

8. "close" rejected

9. wrong session ID rejected

10. wrong phone public key rejected

11. session-close actually terminates active session

12. browser no longer waits for 6.5-second timeout after successful signaling forwarding

If possible, add a protocol-level integration test covering:

remote_request
-> remote_signal offer
-> ACK
-> async answer

==================================================
REAL WINDOWS TEST
==================================================

After implementing:

1. Build the application.
2. Restart the Windows service.
3. Open the web dashboard.
4. Click Remote Desktop.
5. Click Allow Access.
6. Verify no "Request timed out".
7. Verify the PC session-agent starts.
8. Verify WebRTC answer arrives.
9. Verify Remote Desktop remains open.
10. Verify actual screen video appears.
11. Verify authentication succeeds.
12. Verify mouse input works.
13. Exit Remote Desktop.
14. Verify the session-agent process disappears.
15. Verify service remains running normally.

Also test:
- same Wi-Fi
- phone on mobile data if available

==================================================
PERFORMANCE
==================================================

Do not fix the problem by:
- increasing request timeout
- creating polling loops
- repeatedly reconnecting WebRTC
- spawning additional workers
- duplicating PeerConnections

Remote signaling messages are tiny and should remain low overhead.

Keep exactly one Remote Desktop session.

==================================================
FINAL REPORT
==================================================

Report:

- files changed
- exact root cause
- why the original request timed out
- how the ACK/async signaling separation works
- protocol names corrected
- session-close behavior
- tests passed
- actual Windows result
- whether real screen video appeared
- whether WebRTC connected
- whether in-band auth succeeded
- whether mouse input worked
- any remaining issue

Do not call the feature complete until the real screen is visible and the session survives beyond the original request timeout.