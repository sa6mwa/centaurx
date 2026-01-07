package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"pkt.systems/centaurx/bootstrap"
	"pkt.systems/pslog"
)

func newBootstrapCmd() *cobra.Command {
	var outputDir string
	var overwrite bool
	var imageTag string
	var seedUsers bool
	var overrides []string
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Generate default config and container files",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := pslog.Ctx(cmd.Context())
			parsedOverrides, err := parseConfigOverrides(overrides)
			if err != nil {
				return err
			}
			out := outputDir
			if out == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				out = filepath.Join(home, ".centaurx")
			}
			paths, err := bootstrap.WriteBootstrapWithOptions(out, overwrite, imageTag, bootstrap.Options{
				SeedUsers: seedUsers,
				Overrides: parsedOverrides,
			})
			if err != nil {
				return err
			}
			logger.Info("bootstrap", "path", paths.HostConfigPath, "name", "config.yaml")
			logger.Info("bootstrap", "path", paths.Bundle.ConfigPath, "name", "config-for-container.yaml")
			logger.Info("bootstrap", "path", paths.Bundle.ComposePath, "name", "docker-compose.yaml")
			logger.Info("bootstrap", "path", paths.Bundle.PodmanPath, "name", "podman.yaml")
			logger.Info("bootstrap", "path", paths.Bundle.CentaurxContainerfile, "name", "Containerfile.centaurx")
			logger.Info("bootstrap", "path", paths.Bundle.RunnerContainerfile, "name", "Containerfile.cxrunner")
			logger.Info("bootstrap", "path", paths.Bundle.RunnerInstallScript, "name", "cxrunner-install.sh")
			logger.Info("bootstrap", "path", paths.Bundle.SkelDir, "name", "skel/")
			logger.Info("bootstrap", "path", paths.EnvPath, "name", ".env")
			if paths.BinPath != "" {
				logger.Info("bootstrap", "path", paths.BinPath, "name", "centaurx")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "output directory")
	cmd.Flags().BoolVar(&overwrite, "force", false, "overwrite existing files")
	cmd.Flags().StringVar(&imageTag, "image-tag", "", "image tag to use for generated images")
	cmd.Flags().BoolVar(&seedUsers, "seed-users", false, "seed default users (admin)")
	cmd.Flags().StringArrayVarP(&overrides, "config", "c", nil, "config override (e.g. http.base_path=/cx or host:http.base_path=/cx)")
	return cmd
}

func parseConfigOverrides(values []string) ([]bootstrap.ConfigOverride, error) {
	if len(values) == 0 {
		return nil, nil
	}
	overrides := make([]bootstrap.ConfigOverride, 0, len(values))
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid override %q: expected key=value", raw)
		}
		key := strings.TrimSpace(parts[0])
		valueRaw := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("invalid override %q: missing key", raw)
		}
		target := bootstrap.OverrideBoth
		if strings.Contains(key, ":") {
			prefix, rest, _ := strings.Cut(key, ":")
			if strings.TrimSpace(rest) == "" {
				return nil, fmt.Errorf("invalid override %q: missing path", raw)
			}
			switch strings.ToLower(strings.TrimSpace(prefix)) {
			case "host":
				target = bootstrap.OverrideHost
			case "container":
				target = bootstrap.OverrideContainer
			case "both":
				target = bootstrap.OverrideBoth
			default:
				return nil, fmt.Errorf("invalid override %q: unknown target %q", raw, prefix)
			}
			key = strings.TrimSpace(rest)
		}
		if key == "" {
			return nil, fmt.Errorf("invalid override %q: missing path", raw)
		}
		var parsed any
		if err := yaml.Unmarshal([]byte(valueRaw), &parsed); err != nil {
			parsed = valueRaw
		}
		overrides = append(overrides, bootstrap.ConfigOverride{
			Target: target,
			Path:   key,
			Value:  parsed,
		})
	}
	return overrides, nil
}
