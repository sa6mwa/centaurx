// Package runnercontainer manages runner containers for centaurx.
//
// By default it runs one container per user (runner.container_scope: "user"),
// with an optional per-tab mode (runner.container_scope: "tab"). Resource caps
// are applied per container via runner.limits.cpu_percent and
// runner.limits.memory_percent. Niceness settings (runner.exec_nice and
// runner.command_nice) are passed to the runner process to prioritize Codex exec
// relative to shell commands.
package runnercontainer
