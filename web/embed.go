// Package web embeds the built React app (web/dist) into the Go binary.
package web

import (
	"embed"
	"io/fs"
)

// "all:" includes dotfiles, so the committed dist/.gitkeep keeps this
// compiling even before the frontend has been built.
//
//go:embed all:dist
var dist embed.FS

// Dist returns the built app with "dist/" stripped from paths.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // can't happen: "dist" is embedded above
	}
	return sub
}
