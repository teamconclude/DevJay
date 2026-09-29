# Agent attention integration research

## Codex — checked 2026-09-28

### Documented protocol

- [`thread/read`](https://learn.chatgpt.com/docs/app-server#read-a-stored-thread-without-resuming) reads status/history without resuming or subscribing. Status includes `waitingOnApproval` and `waitingOnUserInput`.
- [Starting/resuming](https://learn.chatgpt.com/docs/app-server#start-or-resume-a-thread) joins the event flow; resume accepts configuration overrides. Resume alone does not advance the thread's update timestamp.
- [Approval requests](https://learn.chatgpt.com/docs/app-server#approvals) are server-initiated JSON-RPC requests. Their responses use the original request ID. `serverRequest/resolved` identifies requests that were answered or cleared.
- [Notification opt-out](https://learn.chatgpt.com/docs/app-server#notification-opt-out) does not suppress server requests. It is not a passive-observer mode.
- [Unix/WebSocket transport](https://learn.chatgpt.com/docs/app-server#protocol) is available, but WebSocket support is experimental. Treat version differences and unavailable endpoints as unknown status.
- The documentation does **not** establish pending-request replay on rejoin, first-reply-wins arbitration between clients, or safe disconnect behavior while another client has an unanswered request.

### Installed 0.158.0 evidence

Generated JSON schema from the installed daemon with `codex app-server generate-json-schema --experimental`; no server or turn was started. Relevant artifacts: `ClientRequest.json`, `ToolRequestUserInputParams.json`, `ToolRequestUserInputResponse.json`, `v2/ThreadReadResponse.json`, and `v2/ThreadResumeParams.json`.

- The 167 client methods include no pending-request list/read/respond endpoint or observer-only subscription method.
- `ThreadReadResponse` contains a `Thread`, with no typed pending-request collection or JSON-RPC request ID. `thread/items/list` has the same identity limitation. `Thread.extra` is unspecified and supplies no supported pending-request contract.
- History can contain `agentMessage.questions` (`title`, optional string options). These are not a reliable description of the current blocking request and cannot authorize or route a reply.
- `ThreadResumeParams` says a running thread is rejoined. Optional fields can override settings; a future attach attempt must omit them. This description does not prove replay, arbitration, or disconnect semantics.
- Exact input requests use `item/tool/requestUserInput` with an envelope request ID and `threadId`, `turnId`, `itemId`, `isBlocking`, and `questions`. Each question has `id`, `header`, `question`, optional `options[{label,description}]`, `isOther`, and `isSecret`.
- Input responses are `{answers: {questionID: {answers: [string]}}}`. Command/file approvals use a distinct `decision` response; `decline` and `cancel` have different effects. Do not collapse arbitrary requests into generic Yes/No.
- The read-only live probe found one idle VS Code session on the shared daemon. The observed standalone desktop app used separate stdio IPC. No live rejoin, pending-request response, config change, or transcript-content inspection was performed.
- The installed release contains the executable and generated schema, not reviewable app-server Rust source. Binary diagnostics do not establish the missing lifecycle guarantees.

### Implementable scope and next validation

The current observer can support a brief attention animation, a truthful waiting-type explanation, and navigation to the Site. It cannot show an exact Codex question or safely send a reply from cwd/status alone.

A supported new integration can act as an app-server client for sessions it explicitly manages: retain the live connection, receive each server request, display its actual question/options, and send a response with that request's original ID only after a user action. Bind requests to a specific daemon connection generation, thread, turn, item, and Site; never infer reply destinations from cwd alone.

For existing IDE/CLI sessions, an opt-in rejoin experiment is the next concrete step, using disposable sessions and two clients:

1. Rejoin a running thread with only `threadId`; confirm configuration and the original client remain unchanged.
2. Test joining before and during each request type, whether outstanding requests replay, and how request IDs are scoped.
3. Answer from each client in turn; verify exactly one decision takes effect and both UIs clear through `serverRequest/resolved`.
4. Disconnect/unsubscribe each client, interrupt/complete the turn, and restart the daemon; verify no unintended approval, cancellation, resumed work, or stale controls.
5. Test multiple same-cwd threads and unsupported sources. Keep replies disabled unless the exact identity remains valid.

Until these checks pass against supported versions, keep existing-session detection read-only. Future question text should be bounded, escaped plain text in memory; secret questions need special handling, and resolved/disconnected requests must immediately lose their controls.

## Claude Code — checked 2026-09-28

### Existing-session evidence: installed 2.1.283

The current `~/.claude/sessions/<pid>.json` registry provides `status` and a short `waitingFor` category, not the pending question, options, tool input, or request ID. DevJay verifies the live process identity and cwd before using this status. A waiting observation cannot identify an individual request.

Read-only inspection of the installed executable found:

- The registry's `messagingSocketPath` is a peer-message inbox. Its dispatcher accepts user-message injection and controls for rename, peer-message status, idle notification, and artifact replies. It has no pending-question read or approval-response action. Do not treat it as the SDK control transport.
- SDK permissions and user dialogs live in an in-memory `pendingRequests` map, keyed by request ID. SDK reinitialization can redeliver them, but this is not an independent passive read API for an existing terminal session.
- The existing iTerm `cc-status` integration formats shortened question/permission summaries into iTerm's session status. It does not retain exact choices or an answerable request ID, and its hook-derived status can outlive the actual prompt.

These are version-specific observations, not a supported API contract. No socket connection, subscription, session attach, prompt response, hook/configuration change, or transcript-body read was performed for this investigation.

Reading the newest unresolved transcript `tool_use` would not establish that it is the currently displayed question. The registry does not identify that tool use, and [Claude documents that transcript writes are asynchronous and can lag the live conversation](https://code.claude.com/docs/en/hooks#common-input-fields). Keep current popups descriptive unless a live integration supplies the actual pending request.

### Supported opt-in route for simple approvals

[Channels](https://code.claude.com/docs/en/channels) are a research preview requiring first-party Claude authentication: claude.ai or a Console API key. Pro/Max users can opt in; Team/Enterprise and managed Console organizations need policy approval. Bedrock, Google Cloud's Agent Platform, and Foundry are unsupported. The documented setup restarts the CLI with `--channels`; a terminal inside an IDE can run that CLI, but native IDE-extension/Desktop channel support is not established by these docs.

[Permission relay](https://code.claude.com/docs/en/channels-reference#relay-permission-prompts) supplies a supported request/response mechanism:

- The MCP server declares `experimental['claude/channel'] = {}` and `experimental['claude/channel/permission'] = {}`.
- Claude sends `notifications/claude/channel/permission_request` with string fields `request_id`, `tool_name`, `description`, and `input_preview`.
- A deliberate user action returns `notifications/claude/channel/permission` with the same `request_id` and `behavior: 'allow' | 'deny'`. This decision applies only to that call.
- The terminal prompt remains usable. Claude applies the first answer and drops replies whose IDs are no longer pending. There is no documented resolved/cancel notification or receipt confirming that a verdict took effect; UI must not claim success just because it wrote a notification.
- Custom DevJay channels require the development-channel launch flag during the preview, or an approved plugin/policy path. Existing unconfigured sessions and already-open prompts cannot be passively enrolled.

The installed relay excludes tools whose `requiresUserInteraction()` is true: this includes `AskUserQuestion` and ordinary user-facing `ExitPlanMode`. Its relay previews are sanitized and may be truncated or redact recognized credentials; they are not an exact copy of every native dialog choice. Project trust and MCP consent also stay local. Version 2.1.283 contains the relay; documented security behavior for opted-in channel registration applies from 2.1.234.

### Questions, hooks, and next implementation

For sessions managed by the [Agent SDK](https://code.claude.com/docs/en/agent-sdk/user-input), `canUseTool` receives real tool inputs and cancellation context. `AskUserQuestion` inputs contain `questions` with text, headers, options, and multi-select flags. Answers return through that specific callback as `updatedInput` with the original questions and an `answers` map. This is a supported structured question UI, not a way to attach a new callback to an arbitrary live terminal session.

An alternative [PermissionRequest hook](https://code.claude.com/docs/en/hooks#permissionrequest) receives `tool_name`, `tool_input`, and optional permission suggestions, but no `tool_use_id`. A synchronous hook can wait for a DevJay decision and return allow/deny with optional updated input; while it waits, it delays the native permission flow. Settings-file changes are normally picked up automatically, but do not replay an already-open request. A bridge needs its own per-invocation identity, cancellation/timeout cleanup, and a fail-through path that leaves approval to Claude when DevJay is unavailable. Async hooks cannot supply decisions. Permission suggestions are not the exact visible options.

The smallest next experiment is an opt-in local channel bridge for ordinary CLI tool approvals, tested with a disposable session. Bind every request to the helper connection generation, verified Claude PID/start, session, and Site; keep payloads local and in memory. Test terminal-first and DevJay-first answers, duplicate/stale replies, interruption, disconnects, and same-folder sessions. Only show response buttons while the integration can validate the request; do not derive them from registry state or transcript history. General questions and plan review need a separately validated hook/SDK path.
