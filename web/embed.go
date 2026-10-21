// Package web holds sillage's frontend (a Svelte single-page application
// built by Vite) and exposes its files to the Go binary.
//
// By default the binary embeds the committed placeholder/ directory, so
// that go build and go test work in a clone where Node never ran. Building
// with -tags webdist embeds dist/ instead, which `npm run build` produces;
// without it, that build fails rather than silently shipping the placeholder.
package web

import "io/fs"

// FS returns the frontend files, rooted so that index.html is at the top.
func FS() fs.FS {
	sub, err := fs.Sub(files, filesRoot)
	if err != nil {
		// filesRoot is a constant, valid path: this cannot happen.
		panic(err)
	}
	return sub
}
