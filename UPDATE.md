FIX THE REMOTE DESKTOP SESSION-PIPE STARTUP BUG.

I tested the current implementation and Remote Desktop currently fails immediately with:

"Failed to establish pipe with session worker:
failed to verify session-agent identity:
ImpersonateNamedPipeClient failed:
Unable to impersonate using a named pipe until data has been read from that pipe."

I inspected the current implementation.

ROOT CAUSE:

internal/remote/ipc/pipe_windows.go -> SessionPipeServer.Accept()

currently does:

1. listener.Accept()
2. rootipc.GetClientIdentity(conn.Handle())
3. GetClientIdentity() immediately calls ImpersonateNamedPipeClient()
4. Windows rejects it because the server has not read any message from the pipe yet.

At the same time:

internal/remote/worker/agent.go

ConnectSessionPipe() connects to the pipe and then waits for MsgInitSession from the daemon.

Therefore there is a startup deadlock/order bug:

SERVER:
connect -> impersonate -> fail

WORKER:
connect -> wait for InitSession

The server must read a client message before calling ImpersonateNamedPipeClient().

DO NOT FIX THIS BY REMOVING IMPERSONATION OR DISABLING THE IDENTITY CHECK.

DO NOT weaken the named-pipe ACL.

DO NOT replace this with unauthenticated localhost TCP/WebSocket.

DO NOT trust the handshake message itself as proof of identity.

Implement a proper pipe handshake.

==================================================
REQUIRED FIX
==================================================

Add a dedicated first message from the worker:

MsgAgentHello

The worker MUST send this immediately after connecting to the session pipe, before waiting for InitSession.

The hello should contain at minimum:

{
    type: "agent_hello",
    sessionId: <worker session ID>,
    timestamp: ...
}

Prefer a small typed payload if the existing protocol structure benefits from it.

==================================================
SERVER ACCEPT ORDER
==================================================

Change SessionPipeServer.Accept() to:

1. Accept the named-pipe connection.
2. Create a single bufio.Reader for the connection.
3. Read exactly one newline-delimited handshake envelope from that reader.
4. Bound the handshake size. Do not allow an unbounded read.
5. Parse the envelope.
6. Verify:
   - message type == MsgAgentHello
   - SessionID == expected session's SessionID
   - timestamp/freshness is reasonable
   - malformed/oversized handshake is rejected
7. ONLY AFTER DATA HAS BEEN READ:
   call rootipc.GetClientIdentity(conn.Handle())
8. Verify the actual Windows client identity.
9. Require the worker identity to match the expected AuthorizedUserSID according to the existing security policy.
10. Reject the connection on any failure.
11. Only after identity verification succeeds:
    store the connection and reader in SessionPipeServer.
12. Start readLoop() using THE SAME bufio.Reader instance.

IMPORTANT:
Do not create a second bufio.Reader after the handshake.

The initial handshake bytes may already have been buffered by the first reader. Reusing that reader is required so subsequent messages are not lost.

==================================================
WORKER CONNECT ORDER
==================================================

Change ConnectSessionPipe()/SessionAgent startup so that:

1. Connect to pipe.
2. Immediately send MsgAgentHello.
3. Only then start waiting for MsgInitSession.
4. Start the normal read loop without racing the hello message.

The hello MUST be sent before the worker waits for daemon bootstrap.

Avoid starting the read loop in a way that can accidentally consume its own/other startup state incorrectly.

Preserve existing asynchronous behavior where possible.

==================================================
HANDSHAKE IMPLEMENTATION
==================================================

Prefer reusing the existing:

EncodeEnvelope()

and:

AgentEnvelope

protocol.

Do not invent a second serialization format.

Add a dedicated message constant:

MsgAgentHello AgentMessageType = "agent_hello"

Optionally add:

type AgentHelloPayload struct {
    SessionID string `json:"sessionId"`
    Timestamp int64 `json:"timestamp"`
}

However, keep the actual envelope SessionID field authoritative if that is cleaner.

Do not duplicate SessionID unnecessarily unless there is a concrete validation reason.

==================================================
SECURITY REQUIREMENTS
==================================================

The first message is NOT authentication.

It only exists because Windows requires data to have been read before ImpersonateNamedPipeClient() can identify the client's security context.

The server MUST NOT authorize the worker based solely on:

- session ID
- timestamp
- PID supplied by client
- arbitrary hello fields

Actual Windows identity verification must happen after the handshake read.

The current GetClientIdentity() security behavior must remain intact.

If impersonation fails:
- reject connection
- close pipe
- do not process further messages
- do not send InitSession
- do not spawn/use the worker as trusted

If SID does not match the expected worker identity:
- reject connection
- close pipe

Do not execute any privileged client request before identity verification.

==================================================
HANDSHAKE TIMEOUT
==================================================

Do not allow Accept() to block forever waiting for a hello message.

Use a short bounded handshake timeout.

Possible approach:
- connection deadline if compatible with the existing PipeConn implementation
- or a controlled goroutine/select timeout

Do not introduce a goroutine leak.

A client connecting without sending a hello must be rejected after the timeout.

==================================================
PIPE READER / CONCURRENCY
==================================================

Be careful with the current SessionPipeServer structure.

The current flow is:

Accept()
    -> assigns s.reader
    -> starts readLoop()

After this fix:

Accept()
    -> create reader
    -> read hello with reader
    -> verify Windows identity
    -> assign EXACT SAME reader to s.reader
    -> start readLoop()

Do not have two goroutines reading the same pipe.

Do not have both Accept() and readLoop() concurrently consuming messages.

==================================================
SESSION ID VALIDATION
==================================================

The session worker already receives:

sessionID

when spawned.

The hello must therefore contain the same session ID.

Reject:

- empty session ID
- wrong session ID
- oversized session ID
- malformed JSON
- unsupported message type
- stale/future timestamps outside the existing protocol tolerance

Do not use the client hello to overwrite the server's expected session ID.

==================================================
TESTS
==================================================

Add regression tests.

At minimum:

1. Test hello envelope encoding/decoding.

2. Test that the server requires MsgAgentHello before considering the connection established.

3. Test wrong message type is rejected.

4. Test wrong session ID is rejected.

5. Test malformed hello is rejected.

6. Test oversized hello is rejected.

7. Test stale hello is rejected if timestamp validation is implemented.

8. Test that the SAME buffered reader is reused after the hello.

9. Add a Windows-specific integration test where practical:
   - create session pipe
   - connect worker
   - send hello
   - server reads hello
   - identity verification occurs
   - bootstrap can then be delivered.

Do not rely only on a unit test that mocks away the named-pipe behavior.

==================================================
IMPORTANT: PRESERVE EXISTING PROTOCOL
==================================================

After the hello handshake succeeds, existing messages must continue unchanged:

MsgInitSession
MsgSignalPhoneToAgent
MsgSignalAgentToPhone
MsgAuthSuccess
MsgHeartbeat
MsgTerminate
MsgStatusUpdate

Do not rename or break them.

==================================================
CHECK THE FULL STARTUP SEQUENCE
==================================================

After fixing the named-pipe ordering, trace the COMPLETE sequence:

Browser
 -> remote_request
 -> SessionManager.HandleSessionRequest()
 -> CreateSession()
 -> NewSessionPipeServer()
 -> SpawnSessionAgent()
 -> worker connects
 -> worker sends MsgAgentHello
 -> server reads hello
 -> server impersonates/verifies worker SID
 -> server accepts
 -> server sends MsgInitSession
 -> worker initializes capture/encoder/WebRTC
 -> signaling proceeds
 -> in-band authentication
 -> screen capture starts

Make sure there is no second startup deadlock.

==================================================
DO NOT STOP AT "BUILD PASSES"
==================================================

Run:

go test ./...

and Windows-specific tests where available.

Then build the Windows executable.

Actually launch the service and test:

1. Open dashboard.
2. Click Remote Desktop.
3. Click Allow Access.
4. Verify the old ImpersonateNamedPipeClient error is gone.
5. Verify the worker successfully connects.
6. Verify the service sends InitSession.
7. Verify the Remote Desktop session proceeds to WebRTC signaling.

If the next failure is WebRTC/capture/encoder related, report that separately.

Do not claim the Remote Desktop feature is fixed merely because the named-pipe error disappeared.

==================================================
CODE QUALITY
==================================================

Keep the fix narrow.

Do not rewrite unrelated IPC code.

Do not modify the existing hardened root IPC security unless genuinely necessary.

Do not remove PIPE_REJECT_REMOTE_CLIENTS.

Do not weaken the existing SDDL.

Do not add unnecessary dependencies.

Do not create another pipe implementation.

Reuse the existing root IPC PipeConn and security model.

Finally report:

- exact files changed
- why the bug occurred
- how the handshake fixes Windows impersonation ordering
- tests performed
- result of actual Remote Desktop startup
- any next error if the session now progresses further