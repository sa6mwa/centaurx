package bootstrap

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed files/cxrunner-install.sh files/skel/** templates/*.tmpl
var embeddedFiles embed.FS

func readEmbeddedFile(path string) ([]byte, error) {
	data, err := fs.ReadFile(embeddedFiles, path)
	if err != nil {
		return nil, fmt.Errorf("read embedded %s: %w", path, err)
	}
	return data, nil
}
