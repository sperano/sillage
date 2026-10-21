//go:build webdist

package web

import "embed"

// filesRoot is the embedded directory that FS serves from.
const filesRoot = "dist"

//go:embed dist
var files embed.FS
