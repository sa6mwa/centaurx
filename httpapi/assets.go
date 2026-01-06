package httpapi

import (
	"embed"
	"io/fs"
)

//go:embed assets/*
var embeddedAssets embed.FS

var assetsFS fs.FS

func init() {
	sub, err := fs.Sub(embeddedAssets, "assets")
	if err != nil {
		assetsFS = embeddedAssets
		return
	}
	assetsFS = sub
}
