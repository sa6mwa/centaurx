package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"pkt.systems/pslog"
)

// Run executes a git command in the provided directory.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	log := pslog.Ctx(ctx).With("dir", dir, "args", strings.Join(args, " "))
	log.Debug("git run start")
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		preview := strings.TrimSpace(string(output))
		truncated := false
		if len(preview) > 200 {
			preview = preview[:200]
			truncated = true
		}
		log.Warn("git run failed", "err", err, "output", preview, "truncated", truncated)
		return string(output), fmt.Errorf("git %s failed: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	log.Debug("git run ok", "output_len", len(output))
	return string(output), nil
}

// AddAll stages all changes.
func AddAll(ctx context.Context, dir string) error {
	_, err := Run(ctx, dir, "add", "-A")
	return err
}

// Commit creates a commit with the provided message.
func Commit(ctx context.Context, dir, message string) (string, error) {
	return Run(ctx, dir, "commit", "-m", message)
}
