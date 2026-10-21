// Package web holds the browser UI (a React + TypeScript app in src/) and
// makes its built files available to rbweb.
//
// `npm run build` writes the finished UI into dist/. The line below bundles
// dist/ into the rbweb program at compile time, so the device needs no
// Node.js and serves the UI from memory, fully offline.
//
// dist/ is not committed (only an empty .gitkeep, so the line below finds
// the folder), so run `make ui` before building a release. See
// CONTRIBUTING.md.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFiles embed.FS

// Files returns the built UI, with paths relative to dist/ (so the main
// page is "index.html").
func Files() fs.FS {
	sub, err := fs.Sub(distFiles, "dist")
	if err != nil {
		// Only possible if the embed line above is changed incorrectly.
		panic(err)
	}
	return sub
}
