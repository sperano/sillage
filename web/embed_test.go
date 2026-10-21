package web

import (
	"io/fs"
	"testing"
)

func TestFSHasIndex(t *testing.T) {
	data, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("index.html is empty")
	}
}
