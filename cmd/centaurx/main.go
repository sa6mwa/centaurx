package main

import (
	"context"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"pkt.systems/psi"
	"pkt.systems/pslog"
)

func main() {
	psi.Run(submain)
}

func submain(ctx context.Context) int {
	logger := pslog.LoggerFromEnv(
		pslog.WithEnvWriter(os.Stderr),
		pslog.WithEnvOptions(pslog.Options{Mode: pslog.ModeConsole}),
	)
	ctx = pslog.ContextWithLogger(ctx, logger)
	log.SetOutput(pslog.LogLogger(logger).Writer())
	log.SetFlags(0)

	args := applyArgv0Alias(os.Args)
	root := newRootCmd()
	root.SetArgs(args[1:])

	if err := root.ExecuteContext(ctx); err != nil {
		if !isCodexMockInvocation(args) {
			pslog.Ctx(ctx).With("err", err).Error("centaurx command failed")
		}
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "centaurx",
		Short:         "Centaurx Codex server with SSH and HTTP UIs",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	root.AddCommand(newServeCmd())
	root.AddCommand(newRunnerCmd())
	root.AddCommand(newCodexMockCmd())
	root.AddCommand(newBootstrapCmd())
	root.AddCommand(newBuildCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newDebugCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(newUsersCmd())

	return root
}

func argv0Alias(base string) string {
	switch base {
	case "cxrunner":
		return "runner"
	case "codex-mock", "centaurx-codex-mock":
		return "codex-mock"
	default:
		return ""
	}
}

func applyArgv0Alias(args []string) []string {
	if len(args) == 0 {
		return args
	}
	alias := argv0Alias(filepath.Base(args[0]))
	if alias == "" {
		return args
	}
	out := make([]string, 0, len(args)+1)
	out = append(out, args[0], alias)
	out = append(out, args[1:]...)
	return out
}

func isCodexMockInvocation(args []string) bool {
	return len(args) > 1 && args[1] == "codex-mock"
}
