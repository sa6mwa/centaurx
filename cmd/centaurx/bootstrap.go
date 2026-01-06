package main

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"pkt.systems/centaurx/bootstrap"
	"pkt.systems/pslog"
)

func newBootstrapCmd() *cobra.Command {
	var outputDir string
	var overwrite bool
	cmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Generate default config and container files",
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := pslog.Ctx(cmd.Context())
			out := outputDir
			if out == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				out = filepath.Join(home, ".centaurx")
			}
			paths, err := bootstrap.WriteBootstrap(out, overwrite)
			if err != nil {
				return err
			}
			logger.Info("bootstrap wrote", "path", paths.HostConfigPath, "name", "config.yaml")
			logger.Info("bootstrap wrote", "path", paths.Bundle.ConfigPath, "name", "config-for-container.yaml")
			logger.Info("bootstrap wrote", "path", paths.Bundle.ComposePath, "name", "docker-compose.yaml")
			logger.Info("bootstrap wrote", "path", paths.Bundle.PodmanPath, "name", "podman.yaml")
			logger.Info("bootstrap wrote", "path", paths.Bundle.CentaurxContainerfile, "name", "Containerfile.centaurx")
			logger.Info("bootstrap wrote", "path", paths.Bundle.RunnerContainerfile, "name", "Containerfile.cxrunner")
			logger.Info("bootstrap wrote", "path", paths.Bundle.RunnerInstallScript, "name", "cxrunner-install.sh")
			logger.Info("bootstrap wrote", "path", paths.Bundle.SkelDir, "name", "skel/")
			logger.Info("bootstrap wrote", "path", paths.EnvPath, "name", ".env")
			if paths.BinPath != "" {
				logger.Info("bootstrap wrote", "path", paths.BinPath, "name", "centaurx")
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "output directory")
	cmd.Flags().BoolVar(&overwrite, "force", false, "overwrite existing files")
	return cmd
}
