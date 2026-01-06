package bootstrap

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"

	"pkt.systems/centaurx/internal/appconfig"
	"pkt.systems/centaurx/internal/sshkeys"
	"pkt.systems/centaurx/internal/userhome"
	"pkt.systems/centaurx/internal/version"
	"pkt.systems/centaurx/sshserver"
)

// Files represents generated bootstrap artifacts.
type Files struct {
	ConfigYAML            []byte
	ComposeYAML           []byte
	PodmanYAML            []byte
	CentaurxContainerfile []byte
	RunnerContainerfile   []byte
}

// Assets defines optional bootstrap assets to emit alongside files.
type Assets struct {
	InstallScript []byte
	IncludeSkel   bool
}

// BundlePaths lists output locations for generated artifacts.
type BundlePaths struct {
	ConfigPath            string
	ComposePath           string
	PodmanPath            string
	CentaurxContainerfile string
	RunnerContainerfile   string
	RunnerInstallScript   string
	SkelDir               string
}

// Paths reports where bootstrap wrote its outputs.
type Paths struct {
	HostConfigPath string
	Bundle         BundlePaths
	EnvPath        string
	BinPath        string
}

const (
	containerConfigName       = "config-for-container.yaml"
	runnerInstallRel          = "files/cxrunner-install.sh"
	composeEnvName            = ".env"
	defaultServerImage        = "docker.io/pktsystems/centaurx"
	defaultRunnerImage        = "docker.io/pktsystems/centaurxrunner"
	defaultHostStateTemplate  = "${HOME}/.centaurx/state"
	defaultHostRepoTemplate   = "${HOME}/.centaurx/repos"
	defaultHostConfigTemplate = "${HOME}/.centaurx/config-for-container.yaml"
	defaultPodmanSockTemplate = "/run/user/${UID}/podman/podman.sock"
)

type templateData struct {
	ConfigFile        string
	RunnerInstallPath string
	HostConfigPath    string
	HostStateDir      string
	HostRepoDir       string
	HostPodmanSock    string
	ServerImage       string
}

// DefaultFiles returns container-oriented bootstrap files.
func DefaultFiles() (Files, *Assets, error) {
	hostCfg, err := appconfig.DefaultConfig()
	if err != nil {
		return Files{}, nil, err
	}
	cfg, err := appconfig.DefaultConfig()
	if err != nil {
		return Files{}, nil, err
	}
	cfg.ConfigVersion = appconfig.CurrentConfigVersion
	cfg.RepoRoot = "/cx/repos"
	cfg.StateDir = "/cx/state"
	cfg.Runner.Runtime = "podman"
	cfg.Runner.Image = "docker.io/pktsystems/centaurxrunner:latest"
	cfg.Runner.SockDir = "/cx/state/runner"
	cfg.Runner.RepoRoot = "/cx/repos"
	cfg.Runner.SocketPath = "/cx/state/runner.sock"
	cfg.Runner.Binary = "codex"
	cfg.Runner.KeepaliveIntervalSeconds = 10
	cfg.Runner.KeepaliveMisses = 3
	cfg.Runner.Podman.Address = "unix:///cx/podman.sock"
	tag := resolveImageTag("")
	cfg.Runner.Image = tagImage(defaultRunnerImage, tag)
	cfg.Runner.HostRepoRoot = hostCfg.RepoRoot
	cfg.Runner.HostStateDir = hostCfg.StateDir
	cfg.SSH.HostKeyPath = "/cx/state/ssh/host_key"
	cfg.SSH.KeyStorePath = "/cx/state/ssh/keys.bundle"
	cfg.SSH.KeyDir = "/cx/state/ssh/keys"
	cfg.SSH.AgentDir = "/cx/state/ssh/agent"
	cfg.Auth.UserFile = "/cx/state/users.json"

	configYAML, err := yaml.Marshal(cfg)
	if err != nil {
		return Files{}, nil, err
	}
	tplData := templateData{
		ConfigFile:        containerConfigName,
		RunnerInstallPath: runnerInstallRel,
		HostConfigPath:    filepath.Join(filepath.Dir(hostCfg.StateDir), containerConfigName),
		HostStateDir:      hostCfg.StateDir,
		HostRepoDir:       hostCfg.RepoRoot,
		HostPodmanSock:    defaultPodmanSockPath(),
		ServerImage:       tagImage(defaultServerImage, tag),
	}
	composeYAML, err := renderComposeYAML(tplData)
	if err != nil {
		return Files{}, nil, err
	}
	podmanYAML, err := renderPodmanYAML(tplData)
	if err != nil {
		return Files{}, nil, err
	}
	centaurxFile, err := renderCentaurxContainerfile(tplData)
	if err != nil {
		return Files{}, nil, err
	}
	runnerFile, err := renderRunnerContainerfile(tplData)
	if err != nil {
		return Files{}, nil, err
	}
	runnerScript, err := readEmbeddedFile("files/cxrunner-install.sh")
	if err != nil {
		return Files{}, nil, err
	}

	files := Files{
		ConfigYAML:            configYAML,
		ComposeYAML:           composeYAML,
		PodmanYAML:            podmanYAML,
		CentaurxContainerfile: centaurxFile,
		RunnerContainerfile:   runnerFile,
	}
	return files, &Assets{InstallScript: runnerScript, IncludeSkel: true}, nil
}

// DefaultRepoBundle returns container files intended for repo codegen (no embedded assets).
func DefaultRepoBundle() (Files, *Assets, error) {
	cfg, err := appconfig.DefaultConfig()
	if err != nil {
		return Files{}, nil, err
	}
	cfg.ConfigVersion = appconfig.CurrentConfigVersion
	cfg.RepoRoot = "/cx/repos"
	cfg.StateDir = "/cx/state"
	cfg.Runner.Runtime = "podman"
	cfg.Runner.Image = "docker.io/pktsystems/centaurxrunner:latest"
	cfg.Runner.SockDir = "/cx/state/runner"
	cfg.Runner.RepoRoot = "/cx/repos"
	cfg.Runner.SocketPath = "/cx/state/runner.sock"
	cfg.Runner.Binary = "codex"
	cfg.Runner.KeepaliveIntervalSeconds = 10
	cfg.Runner.KeepaliveMisses = 3
	cfg.Runner.Podman.Address = "unix:///cx/podman.sock"
	tag := resolveImageTag("")
	cfg.Runner.Image = tagImage(defaultRunnerImage, tag)
	cfg.Runner.HostRepoRoot = defaultHostRepoTemplate
	cfg.Runner.HostStateDir = defaultHostStateTemplate
	cfg.SSH.HostKeyPath = "/cx/state/ssh/host_key"
	cfg.SSH.KeyStorePath = "/cx/state/ssh/keys.bundle"
	cfg.SSH.KeyDir = "/cx/state/ssh/keys"
	cfg.SSH.AgentDir = "/cx/state/ssh/agent"
	cfg.Auth.UserFile = "/cx/state/users.json"

	configYAML, err := yaml.Marshal(cfg)
	if err != nil {
		return Files{}, nil, err
	}
	tplData := templateData{
		ConfigFile:        containerConfigName,
		RunnerInstallPath: runnerInstallRel,
		HostConfigPath:    defaultHostConfigTemplate,
		HostStateDir:      defaultHostStateTemplate,
		HostRepoDir:       defaultHostRepoTemplate,
		HostPodmanSock:    defaultPodmanSockTemplate,
		ServerImage:       tagImage(defaultServerImage, tag),
	}
	composeYAML, err := renderComposeYAML(tplData)
	if err != nil {
		return Files{}, nil, err
	}
	podmanYAML, err := renderPodmanYAML(tplData)
	if err != nil {
		return Files{}, nil, err
	}
	centaurxFile, err := renderCentaurxContainerfile(tplData)
	if err != nil {
		return Files{}, nil, err
	}
	runnerFile, err := renderRunnerContainerfile(tplData)
	if err != nil {
		return Files{}, nil, err
	}
	return Files{
		ConfigYAML:            configYAML,
		ComposeYAML:           composeYAML,
		PodmanYAML:            podmanYAML,
		CentaurxContainerfile: centaurxFile,
		RunnerContainerfile:   runnerFile,
	}, nil, nil
}

// DefaultHostConfigYAML returns a host-oriented config YAML.
func DefaultHostConfigYAML() ([]byte, error) {
	cfg, err := DefaultHostConfig()
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(cfg)
}

// DefaultHostConfig returns a host-oriented config.
func DefaultHostConfig() (appconfig.Config, error) {
	cfg, err := appconfig.DefaultConfig()
	if err != nil {
		return appconfig.Config{}, err
	}
	cfg.ConfigVersion = appconfig.CurrentConfigVersion
	cfg.Runner.RepoRoot = "/repos"
	cfg.Runner.HostRepoRoot = cfg.RepoRoot
	cfg.Runner.HostStateDir = cfg.StateDir
	cfg.Runner.Image = tagImage(defaultRunnerImage, resolveImageTag(""))
	return cfg, nil
}

// WriteFiles writes the bootstrap files to the output directory.
func WriteFiles(outputDir string, files Files, overwrite bool) (BundlePaths, error) {
	return WriteFilesWithAssets(outputDir, files, nil, overwrite)
}

// WriteFilesWithAssets writes the files and optional assets to the output directory.
func WriteFilesWithAssets(outputDir string, files Files, assets *Assets, overwrite bool) (BundlePaths, error) {
	if strings.TrimSpace(outputDir) == "" {
		return BundlePaths{}, fmt.Errorf("output directory is required")
	}
	includeAssets := assets != nil && len(assets.InstallScript) > 0
	configPath := filepath.Join(outputDir, containerConfigName)
	composePath := filepath.Join(outputDir, "docker-compose.yaml")
	podmanPath := filepath.Join(outputDir, "podman.yaml")
	centaurxFile := filepath.Join(outputDir, "Containerfile.centaurx")
	runnerFile := filepath.Join(outputDir, "Containerfile.cxrunner")
	runnerInstall := filepath.Join(outputDir, runnerInstallRel)
	skelDir := filepath.Join(outputDir, "files", "skel")

	pathsToCheck := []string{configPath, composePath, podmanPath, centaurxFile, runnerFile}
	if includeAssets {
		pathsToCheck = append(pathsToCheck, runnerInstall)
	}
	for _, path := range pathsToCheck {
		if !overwrite {
			if _, err := os.Stat(path); err == nil {
				return BundlePaths{}, fmt.Errorf("file already exists: %s", path)
			}
		}
	}

	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return BundlePaths{}, err
	}
	if includeAssets {
		if err := os.MkdirAll(filepath.Dir(runnerInstall), 0o755); err != nil {
			return BundlePaths{}, err
		}
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return BundlePaths{}, err
	}
	if err := os.WriteFile(configPath, files.ConfigYAML, 0o644); err != nil {
		return BundlePaths{}, err
	}
	if err := os.WriteFile(composePath, files.ComposeYAML, 0o644); err != nil {
		return BundlePaths{}, err
	}
	podmanYAML := files.PodmanYAML
	if len(podmanYAML) == 0 {
		rootDir := outputDir
		if filepath.IsAbs(outputDir) {
			if abs, err := filepath.Abs(outputDir); err == nil {
				rootDir = abs
			}
		}
		stateDir := filepath.Join(rootDir, "state")
		repoDir := filepath.Join(rootDir, "repos")
		rendered, err := renderPodmanYAML(templateData{
			ConfigFile:     containerConfigName,
			HostConfigPath: filepath.Join(rootDir, containerConfigName),
			HostStateDir:   stateDir,
			HostRepoDir:    repoDir,
			HostPodmanSock: defaultPodmanSockPath(),
			ServerImage:    tagImage(defaultServerImage, resolveImageTag("")),
		})
		if err != nil {
			return BundlePaths{}, err
		}
		podmanYAML = rendered
	}
	if err := os.WriteFile(podmanPath, podmanYAML, 0o644); err != nil {
		return BundlePaths{}, err
	}
	if err := os.WriteFile(centaurxFile, files.CentaurxContainerfile, 0o644); err != nil {
		return BundlePaths{}, err
	}
	if err := os.WriteFile(runnerFile, files.RunnerContainerfile, 0o644); err != nil {
		return BundlePaths{}, err
	}
	if includeAssets {
		if err := os.WriteFile(runnerInstall, assets.InstallScript, 0o755); err != nil {
			return BundlePaths{}, err
		}
		if assets.IncludeSkel {
			if err := copyEmbeddedSkel(skelDir); err != nil {
				return BundlePaths{}, err
			}
		}
	}

	if !includeAssets {
		runnerInstall = ""
		skelDir = ""
	}

	return BundlePaths{
		ConfigPath:            configPath,
		ComposePath:           composePath,
		PodmanPath:            podmanPath,
		CentaurxContainerfile: centaurxFile,
		RunnerContainerfile:   runnerFile,
		RunnerInstallScript:   runnerInstall,
		SkelDir:               skelDir,
	}, nil
}

// WriteBootstrap writes host config plus container bundle outputs.
func WriteBootstrap(outputDir string, overwrite bool, imageTag string) (Paths, error) {
	hostCfg, err := DefaultHostConfig()
	if err != nil {
		return Paths{}, err
	}
	tag := resolveImageTag(imageTag)
	hostCfg.Runner.Image = tagImage(defaultRunnerImage, tag)
	hostPath, err := appconfig.DefaultConfigPath()
	if err != nil {
		return Paths{}, err
	}
	if !overwrite {
		if _, err := os.Stat(hostPath); err == nil {
			return Paths{}, fmt.Errorf("file already exists: %s", hostPath)
		}
	}
	bundle, assets, err := DefaultFiles()
	if err != nil {
		return Paths{}, err
	}
	rootDir, err := filepath.Abs(outputDir)
	if err != nil {
		rootDir = outputDir
	}
	hostStateDir := filepath.Join(rootDir, "state")
	hostRepoDir := filepath.Join(rootDir, "repos")
	if bundle.ConfigYAML, err = overrideContainerConfig(bundle.ConfigYAML, hostStateDir, hostRepoDir, tag); err != nil {
		return Paths{}, err
	}
	tplData := templateData{
		ConfigFile:        containerConfigName,
		RunnerInstallPath: runnerInstallRel,
		HostConfigPath:    filepath.Join(rootDir, containerConfigName),
		HostStateDir:      hostStateDir,
		HostRepoDir:       hostRepoDir,
		HostPodmanSock:    defaultPodmanSockPath(),
		ServerImage:       tagImage(defaultServerImage, tag),
	}
	if bundle.ComposeYAML, err = renderComposeYAML(tplData); err != nil {
		return Paths{}, err
	}
	if bundle.PodmanYAML, err = renderPodmanYAML(tplData); err != nil {
		return Paths{}, err
	}
	paths, err := WriteFilesWithAssets(outputDir, bundle, assets, overwrite)
	if err != nil {
		return Paths{}, err
	}
	envPath, err := writeComposeEnv(outputDir, overwrite)
	if err != nil {
		return Paths{}, err
	}
	if err := ensureHostAssets(hostCfg); err != nil {
		return Paths{}, err
	}
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
		return Paths{}, err
	}
	hostConfig, err := yaml.Marshal(hostCfg)
	if err != nil {
		return Paths{}, err
	}
	if err := os.WriteFile(hostPath, hostConfig, 0o600); err != nil {
		return Paths{}, err
	}
	if err := ensureSeedHomes(hostCfg, paths.SkelDir); err != nil {
		return Paths{}, err
	}
	return Paths{
		HostConfigPath: hostPath,
		Bundle:         paths,
		EnvPath:        envPath,
		BinPath:        "",
	}, nil
}

func ensureSeedHomes(cfg appconfig.Config, skelDir string) error {
	data := userhome.DefaultTemplateData(cfg)
	for _, seed := range cfg.Auth.SeedUsers {
		username := strings.TrimSpace(seed.Username)
		if username == "" {
			continue
		}
		if _, err := userhome.EnsureHome(cfg.StateDir, username, skelDir, data); err != nil {
			return fmt.Errorf("seed home %q: %w", username, err)
		}
	}
	return nil
}

func ensureHostAssets(cfg appconfig.Config) error {
	if _, err := sshkeys.NewStore(cfg.SSH.KeyStorePath, cfg.SSH.KeyDir); err != nil {
		return err
	}
	if _, err := sshserver.EnsureHostKey(cfg.SSH.HostKeyPath); err != nil {
		return err
	}
	return nil
}

func writeComposeEnv(outputDir string, overwrite bool) (string, error) {
	envPath := filepath.Join(outputDir, composeEnvName)
	if !overwrite {
		if _, err := os.Stat(envPath); err == nil {
			return "", fmt.Errorf("file already exists: %s", envPath)
		}
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}
	content := fmt.Sprintf("UID=%d\nGID=%d\n", os.Getuid(), os.Getgid())
	if err := os.WriteFile(envPath, []byte(content), 0o600); err != nil {
		return "", err
	}
	return envPath, nil
}

func defaultPodmanSockPath() string {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = filepath.Join("/run", "user", fmt.Sprintf("%d", os.Getuid()))
	}
	return filepath.Join(runtimeDir, "podman", "podman.sock")
}

func copyEmbeddedSkel(destDir string) error {
	sub, err := fs.Sub(embeddedFiles, "files/skel")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return err
	}
	return fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		name := d.Name()
		if name == ".gitkeep" || name == ".keep" || name == "placeholder.txt" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		clean := filepath.Clean(path)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("invalid skel path: %s", path)
		}
		target := filepath.Join(destDir, clean)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		src, err := sub.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = src.Close() }()
		dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		if _, err := io.Copy(dst, src); err != nil {
			_ = dst.Close()
			return err
		}
		return dst.Close()
	})
}

func renderComposeYAML(data templateData) ([]byte, error) {
	return renderTemplate("templates/docker-compose.yaml.tmpl", data)
}

func renderPodmanYAML(data templateData) ([]byte, error) {
	return renderTemplate("templates/podman.yaml.tmpl", data)
}

func renderCentaurxContainerfile(data templateData) ([]byte, error) {
	return renderTemplate("templates/Containerfile.centaurx.tmpl", data)
}

func renderRunnerContainerfile(data templateData) ([]byte, error) {
	return renderTemplate("templates/Containerfile.cxrunner.tmpl", data)
}

func renderTemplate(name string, data templateData) ([]byte, error) {
	raw, err := readEmbeddedFile(name)
	if err != nil {
		return nil, err
	}
	tpl, err := template.New(filepath.Base(name)).Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

func overrideContainerConfig(configYAML []byte, hostStateDir, hostRepoDir, tag string) ([]byte, error) {
	if len(bytes.TrimSpace(configYAML)) == 0 {
		return configYAML, nil
	}
	var cfg appconfig.Config
	if err := yaml.Unmarshal(configYAML, &cfg); err != nil {
		return nil, err
	}
	if strings.TrimSpace(hostStateDir) != "" {
		cfg.Runner.HostStateDir = hostStateDir
	}
	if strings.TrimSpace(hostRepoDir) != "" {
		cfg.Runner.HostRepoRoot = hostRepoDir
	}
	if strings.TrimSpace(tag) != "" {
		cfg.Runner.Image = tagImage(defaultRunnerImage, tag)
	}
	return yaml.Marshal(cfg)
}

func resolveImageTag(override string) string {
	if value := strings.TrimSpace(override); value != "" {
		return value
	}
	value := strings.TrimSpace(version.Current())
	if value == "" {
		return "v0.0.0-unknown"
	}
	return value
}

func tagImage(base, tag string) string {
	base = stripImageTag(base)
	if base == "" {
		return ""
	}
	if strings.TrimSpace(tag) == "" {
		tag = "v0.0.0-unknown"
	}
	return base + ":" + tag
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
