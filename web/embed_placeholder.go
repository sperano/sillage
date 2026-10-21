//go:build !webdist

package web

import "embed"

// filesRoot is the embedded directory that FS serves from.
const filesRoot = "placeholder"

//go:embed placeholder
var files embed.FS
