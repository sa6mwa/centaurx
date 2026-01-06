# Runner gRPC Spec (Option 2: Structured Events)

This document defines the gRPC contract between the centaurx server and the centaurx runner
process. It is a spec only (no code). The goal is to keep formatting and UX logic in
the server while the runner focuses on process execution and streaming structured events.

The canonical proto lives at `proto/runner/v1/runner.proto`.

## Goals

- Split runner into a separate process/container (`centaurx runner`).
- Use gRPC over Unix domain sockets (UDS) only.
- Stream structured events (not formatted lines, not raw JSONL).
- Support codex exec (new session and resume), shell commands (`!`), and stop signals.
- Keep tests runnable without containers (local binaries + UDS).

## Non-goals

- No TCP transport.
- No auth in the gRPC layer (UDS + file permissions only).
- No UI formatting in the runner.

## Service overview

```
service Runner {
  rpc Exec(ExecRequest) returns (stream RunnerEvent);
  rpc ExecResume(ExecResumeRequest) returns (stream RunnerEvent);
  rpc RunCommand(RunCommandRequest) returns (stream RunnerEvent);
  rpc SignalSession(SignalRequest) returns (SignalResponse);
}
```

### Run identity

- The client (server) supplies `run_id` and must ensure uniqueness per runner.
- The runner echoes `run_id` in all events and uses it for signal routing.
- `run_id` should be stable for the lifetime of a single process invocation.

## Messages (proposed proto shapes)

```
message ExecRequest {
  string run_id = 1;           // required; unique
  string working_dir = 2;      // required; repo path
  string prompt = 3;           // required; entire prompt text
  string model = 4;            // optional; empty = default
  bool json = 5;               // required; should be true
  string ssh_auth_sock = 6;    // optional; per-user SSH_AUTH_SOCK
}

message ExecResumeRequest {
  string run_id = 1;           // required; unique
  string working_dir = 2;      // required; repo path
  string prompt = 3;           // required; entire prompt text
  string model = 4;            // optional; empty = default
  string resume_session_id = 5; // required; thread_id
  bool json = 6;               // required; should be true
  string ssh_auth_sock = 7;    // optional; per-user SSH_AUTH_SOCK
}

message RunCommandRequest {
  string run_id = 1;           // required; unique
  string working_dir = 2;      // required; repo path
  string command = 3;          // required; full command line
  bool use_shell = 4;          // default true; run "sh -lc <command>"
  string ssh_auth_sock = 5;    // optional; per-user SSH_AUTH_SOCK
}

message SignalRequest {
  string run_id = 1;           // required
  ProcessSignal signal = 2;    // HUP | TERM | KILL
}

message SignalResponse {
  bool ok = 1;
  string message = 2;          // optional details (not found, already exited)
}

enum ProcessSignal {
  PROCESS_SIGNAL_UNSPECIFIED = 0;
  PROCESS_SIGNAL_HUP = 1;
  PROCESS_SIGNAL_TERM = 2;
  PROCESS_SIGNAL_KILL = 3;
}
```

### Event envelope

```
message RunnerEvent {
  string run_id = 1;
  oneof payload {
    ExecEvent exec = 2;             // codex exec event (structured)
    CommandOutput command_output = 3; // stdout/stderr for shell commands
    RunStatus status = 4;           // started/finished/failed
  }
}
```

### Run lifecycle status

```
message RunStatus {
  RunState state = 1;             // STARTED | FINISHED | FAILED
  int32 exit_code = 2;            // set on FINISHED or FAILED
  string message = 3;             // optional details
}

enum RunState {
  RUN_STATE_UNSPECIFIED = 0;
  RUN_STATE_STARTED = 1;
  RUN_STATE_FINISHED = 2;
  RUN_STATE_FAILED = 3;
}
```

### Command output

```
message CommandOutput {
  StreamKind stream = 1;          // STDOUT | STDERR
  string text = 2;                // chunk (not necessarily line)
}

enum StreamKind {
  STREAM_KIND_UNSPECIFIED = 0;
  STREAM_KIND_STDOUT = 1;
  STREAM_KIND_STDERR = 2;
}
```

### Exec event (matches model.ExecEvent)

```
message ExecEvent {
  EventType type = 1;
  string thread_id = 2;
  TurnUsage usage = 3;
  ItemEvent item = 4;
  ErrorEvent error = 5;
  string message = 6;             // e.g. warning text
  bytes raw = 7;                  // optional raw JSON for unknown fields
}

message TurnUsage {
  int32 input_tokens = 1;
  int32 cached_input_tokens = 2;
  int32 output_tokens = 3;
}

message ItemEvent {
  string id = 1;
  ItemType type = 2;
  string text = 3;
  string command = 4;
  string aggregated_output = 5;
  int32 exit_code = 6;            // optional, only if set
  string status = 7;
  repeated FileChange changes = 8;
  string query = 9;
  repeated TodoItem items = 10;
  bytes raw = 11;                 // optional raw JSON for unknown fields
}

message FileChange {
  string path = 1;
  string kind = 2;
}

message TodoItem {
  string text = 1;
  bool completed = 2;
}

message ErrorEvent {
  string message = 1;
}

enum EventType {
  EVENT_TYPE_UNSPECIFIED = 0;
  EVENT_THREAD_STARTED = 1;
  EVENT_TURN_STARTED = 2;
  EVENT_TURN_COMPLETED = 3;
  EVENT_TURN_FAILED = 4;
  EVENT_ITEM_STARTED = 5;
  EVENT_ITEM_UPDATED = 6;
  EVENT_ITEM_COMPLETED = 7;
  EVENT_ERROR = 8;
}

enum ItemType {
  ITEM_TYPE_UNSPECIFIED = 0;
  ITEM_AGENT_MESSAGE = 1;
  ITEM_REASONING = 2;
  ITEM_COMMAND_EXECUTION = 3;
  ITEM_FILE_CHANGE = 4;
  ITEM_MCP_TOOL_CALL = 5;
  ITEM_WEB_SEARCH = 6;
  ITEM_TODO_LIST = 7;
  ITEM_ERROR = 8;
}
```

## Call flows

### New session (codex exec)

1) Server calls `Exec` with `run_id`, `working_dir`, `prompt`, `model`.
2) Runner emits `RunStatus{STARTED}`.
3) Runner streams `ExecEvent` messages parsed from JSONL.
4) On process exit, runner emits `RunStatus{FINISHED, exit_code}` and closes the stream.

### Resume session (codex exec resume)

Same as above, but server calls `ExecResume` with `resume_session_id` (thread id).

### Run shell command (`!`)

1) Server calls `RunCommand` with `command` and `working_dir`.
2) Runner emits `RunStatus{STARTED}`.
3) Runner streams `CommandOutput` for stdout/stderr as chunks.
4) Runner emits `RunStatus{FINISHED, exit_code}` and closes the stream.

### Stop session

1) Server calls `SignalSession` with `run_id` and `signal` (TERM/KILL).
2) Runner replies `ok` if it found and signaled the process.
3) Runner may also emit `RunStatus{FAILED}` on the stream if the process exits due to signal.

Server `/z` logic:
- Send SIGTERM, wait 10s
- If still running, send SIGKILL

## Error handling

- Validation errors (missing `run_id`, invalid `working_dir`) return gRPC error status.
- Process launch failures are sent as `RunStatus{FAILED}` with message and exit code if known.
- Unknown JSONL event shapes should be preserved in `raw`.

## Transport

- UDS path is config-driven (e.g. `~/.centaurx/state/runner.sock`).
- File permissions control access (no additional auth).
- Tests must run without containers: start runner in-process or as a subprocess using a temp UDS.
