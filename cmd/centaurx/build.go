package main

import (
	"context"
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"pkt.systems/centaurx/bootstrap"
	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/internal/shipohoy"
	"pkt.systems/centaurx/internal/shipohoy/buildkit"
	"pkt.systems/centaurx/internal/shipohoy/containerd"
	"pkt.systems/centaurx/internal/shipohoy/podman"
	"pkt.systems/centaurx/internal/version"
	"pkt.systems/pslog"
)

const defaultServerImage = "docker.io/pktsystems/centaurx:latest"

type buildSharedOptions struct {
	configPath      string
	binPath         string
	namespace       string
	disableImport   bool
	redistributable bool
}

func newBuildCmd() *cobra.Command {
	opts := &buildSharedOptions{}
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build container images",
	}
	cmd.PersistentFlags().StringVarP(&opts.configPath, "config", "c", "", "path to config file")
	cmd.PersistentFlags().StringVar(&opts.binPath, "bin", "", "path to centaurx binary")
	cmd.PersistentFlags().StringVar(&opts.namespace, "namespace", "", "override containerd namespace for import (containerd only)")
	cmd.PersistentFlags().BoolVar(&opts.disableImport, "disable-import", false, "skip importing the built image into containerd (containerd only)")
	cmd.PersistentFlags().BoolVar(&opts.redistributable, "redistributable", false, "build redistributable runner image (excludes non-redistributable tooling)")

	cmd.AddCommand(newBuildServerCmd(opts))
	cmd.AddCommand(newBuildRunnerCmd(opts))
	cmd.AddCommand(newBuildAllCmd(opts))
	return cmd
}

func newBuildServerCmd(shared *buildSharedOptions) *cobra.Command {
	var tag string
	var output string
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Build the centaurx server image",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, configPath, err := loadRequiredConfig(shared.configPath)
			if err != nil {
				return err
			}
			tags, err := buildTags(defaultServerImage, tag)
			if err != nil {
				return err
			}
			builder, runtimeKind, err := selectBuilder(cfg)
			if err != nil {
				return err
			}
			outputPath, err := resolveOutputPath(configPath, output, "pktsystems-centaurx.oci.tar")
			if err != nil {
				return err
			}
			return buildServer(cmd.Context(), cfg, builder, runtimeKind, shared, tags, outputPath)
		},
	}
	cmd.Flags().StringVarP(&tag, "tag", "t", "", "image tag (default: version + latest)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "path to OCI tar export (default: <config dir>/pktsystems-centaurx.oci.tar)")
	return cmd
}

func newBuildRunnerCmd(shared *buildSharedOptions) *cobra.Command {
	var tag string
	var output string
	cmd := &cobra.Command{
		Use:   "runner",
		Short: "Build the cxrunner image",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, configPath, err := loadRequiredConfig(shared.configPath)
			if err != nil {
				return err
			}
			tags, err := buildRunnerTags(cfg.Runner.Image, tag, shared.redistributable)
			if err != nil {
				return err
			}
			builder, runtimeKind, err := selectBuilder(cfg)
			if err != nil {
				return err
			}
			outputPath, err := resolveOutputPath(configPath, output, "pktsystems-centaurxrunner.oci.tar")
			if err != nil {
				return err
			}
			return buildRunner(cmd.Context(), cfg, builder, runtimeKind, shared, tags, outputPath)
		},
	}
	cmd.Flags().StringVarP(&tag, "tag", "t", "", "image tag (default: version + latest)")
	cmd.Flags().StringVarP(&output, "output", "o", "", "path to OCI tar export (default: <config dir>/pktsystems-centaurxrunner.oci.tar)")
	return cmd
}

func newBuildAllCmd(shared *buildSharedOptions) *cobra.Command {
	var serverTag string
	var runnerTag string
	cmd := &cobra.Command{
		Use:     "all",
		Aliases: []string{"both"},
		Short:   "Build both server and runner images",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, configPath, err := loadRequiredConfig(shared.configPath)
			if err != nil {
				return err
			}
			serverTags, err := buildTags(defaultServerImage, serverTag)
			if err != nil {
				return err
			}
			runnerTags, err := buildRunnerTags(cfg.Runner.Image, runnerTag, shared.redistributable)
			if err != nil {
				return err
			}
			builder, runtimeKind, err := selectBuilder(cfg)
			if err != nil {
				return err
			}
			serverOutput, err := resolveOutputPath(configPath, "", "pktsystems-centaurx.oci.tar")
			if err != nil {
				return err
			}
			runnerOutput, err := resolveOutputPath(configPath, "", "pktsystems-centaurxrunner.oci.tar")
			if err != nil {
				return err
			}
			if err := buildServer(cmd.Context(), cfg, builder, runtimeKind, shared, serverTags, serverOutput); err != nil {
				return err
			}
			return buildRunner(cmd.Context(), cfg, builder, runtimeKind, shared, runnerTags, runnerOutput)
		},
	}
	cmd.Flags().StringVar(&serverTag, "server-tag", "", "server image tag (default: version + latest)")
	cmd.Flags().StringVar(&runnerTag, "runner-tag", "", "runner image tag (default: version + latest)")
	return cmd
}

func buildServer(ctx context.Context, cfg appconfig.Config, builder shipohoy.Builder, runtimeKind string, shared *buildSharedOptions, tags []string, outputPath string) error {
	logger := pslog.Ctx(ctx)
	binPath, err := resolveCentaurxBinary(shared.binPath)
	if err != nil {
		return err
	}
	if err := ensureStaticBinary(binPath); err != nil {
		return err
	}
	contextDir, cleanup, err := prepareServerContext(binPath)
	if err != nil {
		return err
	}
	defer cleanup()

	files, _, err := bootstrap.DefaultFiles()
	if err != nil {
		return err
	}
	spec := shipohoy.BuildSpec{
		ContextDir:        contextDir,
		ContainerfileData: files.CentaurxContainerfile,
		Tags:              tags,
		BuildArgs: map[string]string{
			"CENTAURX_BIN": "bin/centaurx",
		},
		Timeout:    buildTimeout(cfg),
		OutputPath: outputPath,
	}
	logger.Info("build.start", "target", "server", "tags", tags, "output", outputPath)
	_, err = runBuild(ctx, builder, spec, logger)
	if err != nil {
		return err
	}
	return postBuild(ctx, cfg, runtimeKind, shared, outputPath, spec.Tags)
}

func buildRunner(ctx context.Context, cfg appconfig.Config, builder shipohoy.Builder, runtimeKind string, shared *buildSharedOptions, tags []string, outputPath string) error {
	logger := pslog.Ctx(ctx)
	binPath, err := resolveCentaurxBinary(shared.binPath)
	if err != nil {
		return err
	}
	files, assets, err := bootstrap.DefaultFiles()
	if err != nil {
		return err
	}
	if assets == nil || len(assets.InstallScript) == 0 {
		return errors.New("runner install script missing")
	}
	contextDir, cleanup, err := prepareRunnerContext(binPath, assets.InstallScript)
	if err != nil {
		return err
	}
	defer cleanup()

	spec := shipohoy.BuildSpec{
		ContextDir:        contextDir,
		ContainerfileData: files.RunnerContainerfile,
		Tags:              tags,
		BuildArgs: map[string]string{
			"RUNNER_INSTALL": "files/cxrunner-install.sh",
			"BIN_DIR":        "bin",
		},
		Timeout:    buildTimeout(cfg),
		OutputPath: outputPath,
	}
	if shared != nil && shared.redistributable {
		spec.BuildArgs["CX_REDISTRIBUTABLE"] = "1"
	}
	logger.Info("build.start", "target", "runner", "tags", tags, "output", outputPath)
	_, err = runBuild(ctx, builder, spec, logger)
	if err != nil {
		return err
	}
	return postBuild(ctx, cfg, runtimeKind, shared, outputPath, spec.Tags)
}

func runBuild(ctx context.Context, builder shipohoy.Builder, spec shipohoy.BuildSpec, logger pslog.Logger) (shipohoy.BuildResult, error) {
	if withEvents, ok := builder.(shipohoy.BuilderWithEvents); ok {
		events := make(chan shipohoy.BuildEvent, 256)
		done := make(chan struct{})
		go func() {
			defer close(done)
			logBuildEvents(ctx, logger, events)
		}()
		res, err := withEvents.BuildWithEvents(ctx, spec, events)
		close(events)
		<-done
		if err == nil {
			logger.Info("build.complete", "images", res.ImageNames)
		}
		return res, err
	}
	res, err := builder.Build(ctx, spec)
	if err == nil {
		logger.Info("build.complete", "images", res.ImageNames)
	}
	return res, err
}

func logBuildEvents(ctx context.Context, logger pslog.Logger, events <-chan shipohoy.BuildEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			switch ev.Kind {
			case shipohoy.BuildEventVertexStarted:
				msg := buildEventMessage(ev, "build.event")
				logger.Info(msg, "state", "started")
			case shipohoy.BuildEventVertexCompleted:
				msg := buildEventMessage(ev, "build.event")
				if ev.Error != "" {
					logger.Error(msg, "vertex", ev.VertexID, "err", ev.Error)
				} else {
					logger.Info(msg, "state", "completed")
				}
			case shipohoy.BuildEventLog:
				line := strings.TrimSpace(ev.Message)
				if line == "" {
					line = buildEventMessage(ev, "build.event")
				}
				logger.Info(line)
			case shipohoy.BuildEventWarning:
				msg := buildEventMessage(ev, "build.event")
				logger.Warn(msg, "warning", ev.Message)
			default:
				msg := buildEventMessage(ev, "build.event")
				logger.Info(msg, "kind", ev.Kind, "msg", ev.Message)
			}
		}
	}
}

func buildEventMessage(ev shipohoy.BuildEvent, fallback string) string {
	if strings.TrimSpace(ev.Name) != "" {
		return ev.Name
	}
	return fallback
}

func buildTimeout(cfg appconfig.Config) time.Duration {
	if cfg.Runner.BuildTimeout <= 0 {
		return 0
	}
	return time.Duration(cfg.Runner.BuildTimeout) * time.Minute
}

func selectBuilder(cfg appconfig.Config) (shipohoy.Builder, string, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Runner.Runtime)) {
	case "podman":
		return podman.NewBuilder(podman.Config{Address: cfg.Runner.Podman.Address}), "podman", nil
	case "containerd":
		return buildkit.New(buildkit.Config{Address: cfg.Runner.BuildKit.Address}), "containerd", nil
	default:
		return nil, "", fmt.Errorf("unsupported runner.runtime %q", cfg.Runner.Runtime)
	}
}

func postBuild(ctx context.Context, cfg appconfig.Config, runtimeKind string, shared *buildSharedOptions, outputPath string, images []string) error {
	switch runtimeKind {
	case "containerd":
		if err := importBuildOutputContainerd(ctx, cfg, shared, outputPath, images); err != nil {
			return err
		}
		return verifyBuiltImagesContainerd(ctx, cfg, shared, images)
	case "podman":
		if shared != nil {
			logger := pslog.Ctx(ctx)
			if shared.disableImport {
				logger.Info("build.import.skipped", "reason", "podman backend")
			}
			if strings.TrimSpace(shared.namespace) != "" {
				logger.Info("build.namespace.ignored", "namespace", shared.namespace, "reason", "podman backend")
			}
		}
		return verifyBuiltImagesPodman(ctx, cfg, images)
	default:
		return fmt.Errorf("unsupported runtime %q", runtimeKind)
	}
}

func verifyBuiltImagesContainerd(ctx context.Context, cfg appconfig.Config, shared *buildSharedOptions, images []string) error {
	if shared != nil && shared.disableImport {
		return nil
	}
	if len(images) == 0 {
		return nil
	}
	namespace := cfg.Runner.Containerd.Namespace
	if shared != nil && strings.TrimSpace(shared.namespace) != "" {
		namespace = shared.namespace
	}
	runtime, err := containerd.New(ctx, containerd.Config{
		Address:     cfg.Runner.Containerd.Address,
		Namespace:   namespace,
		PullTimeout: time.Duration(cfg.Runner.PullTimeout) * time.Minute,
	})
	if err != nil {
		return err
	}
	defer func() { _ = runtime.Close() }()
	for _, image := range images {
		ok, err := runtime.ImageExists(ctx, image)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("image %q not found in containerd namespace %q; import failed or namespace mismatch", image, namespace)
		}
	}
	return nil
}

func verifyBuiltImagesPodman(ctx context.Context, cfg appconfig.Config, images []string) error {
	if len(images) == 0 {
		return nil
	}
	runtime, err := podman.New(ctx, podman.Config{
		Address:     cfg.Runner.Podman.Address,
		UserNSMode:  cfg.Runner.Podman.UserNSMode,
		PullTimeout: time.Duration(cfg.Runner.PullTimeout) * time.Minute,
	})
	if err != nil {
		return err
	}
	for _, image := range images {
		ok, err := runtime.ImageExists(ctx, image)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("image %q not found in podman store", image)
		}
	}
	return nil
}

func loadRequiredConfig(path string) (appconfig.Config, string, error) {
	configPath, err := resolveConfigPath(path)
	if err != nil {
		return appconfig.Config{}, "", err
	}
	if _, err := os.Stat(configPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return appconfig.Config{}, "", fmt.Errorf("config not found: %s; run centaurx bootstrap", configPath)
		}
		return appconfig.Config{}, "", err
	}
	cfg, err := appconfig.Load(configPath)
	if err != nil {
		return appconfig.Config{}, "", err
	}
	return cfg, configPath, nil
}

func resolveConfigPath(path string) (string, error) {
	configPath := strings.TrimSpace(path)
	if configPath != "" {
		return configPath, nil
	}
	return appconfig.DefaultConfigPath()
}

func resolveOutputPath(configPath string, override string, filename string) (string, error) {
	output := strings.TrimSpace(override)
	if output == "" {
		dir := filepath.Dir(configPath)
		output = filepath.Join(dir, "containers", filename)
	}
	if strings.TrimSpace(output) == "" {
		return "", errors.New("output path is required")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return "", err
	}
	return output, nil
}

func importBuildOutputContainerd(ctx context.Context, cfg appconfig.Config, shared *buildSharedOptions, outputPath string, tags []string) error {
	logger := pslog.Ctx(ctx)
	if shared != nil && shared.disableImport {
		logger.Info("build.import.skipped", "path", outputPath)
		return nil
	}
	if strings.TrimSpace(outputPath) == "" {
		return errors.New("output path is required for import")
	}
	namespace := cfg.Runner.Containerd.Namespace
	if shared != nil && strings.TrimSpace(shared.namespace) != "" {
		namespace = shared.namespace
	}
	runtime, err := containerd.New(ctx, containerd.Config{
		Address:     cfg.Runner.Containerd.Address,
		Namespace:   namespace,
		PullTimeout: time.Duration(cfg.Runner.PullTimeout) * time.Minute,
	})
	if err != nil {
		return err
	}
	defer func() { _ = runtime.Close() }()
	logger.Info("build.import.start", "path", outputPath, "namespace", namespace)
	if err := runtime.Import(ctx, outputPath, tags); err != nil {
		return err
	}
	logger.Info("build.import.complete", "path", outputPath, "namespace", namespace)
	return nil
}

func buildTags(baseImage string, override string) ([]string, error) {
	if value := strings.TrimSpace(override); value != "" {
		return []string{value}, nil
	}
	base := stripImageTag(baseImage)
	if base == "" {
		return nil, errors.New("image name is required")
	}
	ver := version.Current()
	if strings.TrimSpace(ver) == "" {
		ver = "v0.0.0-unknown"
	}
	return []string{
		base + ":" + ver,
		base + ":latest",
	}, nil
}

func buildRunnerTags(baseImage string, override string, redistributable bool) ([]string, error) {
	if value := strings.TrimSpace(override); value != "" {
		return []string{value}, nil
	}
	base := stripImageTag(baseImage)
	if base == "" {
		return nil, errors.New("image name is required")
	}
	ver := version.Current()
	if strings.TrimSpace(ver) == "" {
		ver = "v0.0.0-unknown"
	}
	if redistributable {
		return []string{
			base + ":" + ver + "-redistributable",
			base + ":redistributable",
		}, nil
	}
	return []string{
		base + ":" + ver,
		base + ":latest",
	}, nil
}

func stripImageTag(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	if at := strings.LastIndex(image, "@"); at != -1 {
		image = image[:at]
	}
	lastSlash := strings.LastIndex(image, "/")
	lastColon := strings.LastIndex(image, ":")
	if lastColon > lastSlash {
		return image[:lastColon]
	}
	return image
}

func resolveCentaurxBinary(explicit string) (string, error) {
	if value := strings.TrimSpace(explicit); value != "" {
		return ensureFile(value)
	}
	if value := strings.TrimSpace(os.Getenv("CENTAURX_BIN")); value != "" {
		return ensureFile(value)
	}
	if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
		return ensureFile(exe)
	}
	if path, err := exec.LookPath("centaurx"); err == nil && strings.TrimSpace(path) != "" {
		return ensureFile(path)
	}
	return "", errors.New("centaurx binary not found; use --bin or set CENTAURX_BIN")
}

func ensureFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", path)
	}
	return path, nil
}

func prepareServerContext(binPath string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "centaurx-build-server-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := copyFile(binPath, filepath.Join(binDir, "centaurx"), 0o755); err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}

func prepareRunnerContext(binPath string, installScript []byte) (string, func(), error) {
	dir, err := os.MkdirTemp("", "centaurx-build-runner-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	binDir := filepath.Join(dir, "bin")
	filesDir := filepath.Join(dir, "files")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := copyFile(binPath, filepath.Join(binDir, "centaurx"), 0o755); err != nil {
		cleanup()
		return "", nil, err
	}
	scriptPath := filepath.Join(filesDir, "cxrunner-install.sh")
	if err := os.WriteFile(scriptPath, installScript, 0o755); err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func ensureStaticBinary(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	ef, err := elf.NewFile(file)
	if err != nil {
		return fmt.Errorf("centaurx binary is not a valid ELF file: %w", err)
	}
	for _, prog := range ef.Progs {
		if prog.Type == elf.PT_INTERP {
			return errors.New("centaurx binary is dynamically linked; scratch image requires a static binary (build with CGO_ENABLED=0)")
		}
	}
	return nil
}
