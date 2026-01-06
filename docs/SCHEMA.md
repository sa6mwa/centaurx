As of today, Codex CLI does **not** ship an official JSON Schema file for the **event stream** you get from `codex exec --json` / `--experimental-json`.

What they *do* provide is a *description* of the JSONL event stream (event types + item types) in docs, but it’s not published as a machine-consumable schema artifact. ([GitHub][1]) And there’s an open request asking for exactly what you want (“Provide JSON Schema for --json flag output”). ([GitHub][2]) There’s also evidence the stream format has changed in ways that broke parsers relying on the docs. ([GitHub][3])

### What to do instead (recommended)

If your goal is “serialize in my app by schema” (i.e., a stable contract), don’t treat `--json` as that contract. Use `--output-schema <path>` to force the **final stdout payload** to conform to a JSON Schema you control. That’s explicitly supported and documented. ([OpenAI Developers][4])

### If you still want the streaming `--json` events

Then you should parse it as a **discriminated union** keyed by the top-level `"type"` field, and be deliberately tolerant:

* Treat unknown event types / item fields as forward-compatible (e.g., `map[string]any` / `json.RawMessage`).
* Version-gate behavior by the installed Codex CLI version (since the format has changed). ([GitHub][3])
* Base your initial union on their documented set:

  * Events: `thread.started`, `turn.started`, `turn.completed`, `turn.failed`, `item.started|updated|completed`, `error`
  * Items: `agent_message`, `reasoning`, `command_execution`, `file_change`, `mcp_tool_call`, `web_search`, `todo_list` ([GitHub][1])

If you want, I can sketch a pragmatic JSON Schema *you* can adopt for the event stream (with `additionalProperties: true` and per-`type` branches) — but it would be **your** compatibility layer, not an official upstream contract.

[1]: https://raw.githubusercontent.com/openai/codex/main/docs/exec.md "raw.githubusercontent.com"
[2]: https://github.com/openai/codex/issues/1673 "Provide JSON Schema for --json flag output · Issue #1673 · openai/codex · GitHub"
[3]: https://github.com/openai/codex/issues/4776 "JSON output mode docs are out of date · Issue #4776 · openai/codex · GitHub"
[4]: https://developers.openai.com/codex/sdk/ "Codex SDK"

---

See `example.jsonl` for the example output of `codex exec --json "prompt" > output.jsonl`.
