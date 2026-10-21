module github.com/sperano/sillage

go 1.27

// web/node_modules is npm's, not Go's: keep ./... patterns out of it.
ignore ./web/node_modules
