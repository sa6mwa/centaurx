package main

import (
	"flag"
	"fmt"
	"os"

	"pkt.systems/centaurx/bootstrap"
)

func main() {
	var output string
	var overwrite bool
	var seedUsers bool
	flag.StringVar(&output, "output", ".", "output directory")
	flag.StringVar(&output, "o", ".", "output directory")
	flag.BoolVar(&overwrite, "force", false, "overwrite existing files")
	flag.BoolVar(&seedUsers, "seed-users", false, "seed default users (admin)")
	flag.Parse()

	files, assets, err := bootstrap.DefaultRepoBundleWithOptions(bootstrap.Options{
		SeedUsers: seedUsers,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	paths, err := bootstrap.WriteFilesWithAssets(output, files, assets, overwrite)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, paths.ConfigPath)
	fmt.Fprintln(os.Stdout, paths.ComposePath)
	fmt.Fprintln(os.Stdout, paths.PodmanPath)
	fmt.Fprintln(os.Stdout, paths.CentaurxContainerfile)
	fmt.Fprintln(os.Stdout, paths.RunnerContainerfile)
	if paths.RunnerInstallScript != "" {
		fmt.Fprintln(os.Stdout, paths.RunnerInstallScript)
	}
	if paths.SkelDir != "" {
		fmt.Fprintln(os.Stdout, paths.SkelDir)
	}
}
