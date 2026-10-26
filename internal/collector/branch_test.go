package collector

import (
	"testing"

	"github.com/sperano/sillage/internal/model"
)

func TestRootOfBranch(t *testing.T) {
	cfg := Config{VikunjaInstance: "vikunja.example.com"}
	task := model.Root{Task: &model.TaskKey{Instance: "vikunja.example.com", ID: 42}}
	tests := []struct {
		branch string
		want   *model.Root
	}{
		{"kelos/vikunja-42", &task},
		{"kelos/wiki-7", nil},
		{"kelos/vikunja-042", nil},
		{"kelos/vikunja-0", nil},
		{"kelos/vikunja--42", nil},
		{"kelos/vikunja-42x", nil},
		{"feature/vikunja-42", nil},
		{"kelos/vikunja-99999999999999999999", nil},
		{"main", nil},
		{"", nil},
	}
	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got, ok := cfg.RootOfBranch(tt.branch)
			switch {
			case tt.want == nil && ok:
				t.Errorf("RootOfBranch(%q) = %+v, want none", tt.branch, got)
			case tt.want != nil && !ok:
				t.Errorf("RootOfBranch(%q) names no root, want %+v", tt.branch, *tt.want)
			case tt.want != nil && rootString(got) != rootString(*tt.want):
				t.Errorf("RootOfBranch(%q) = %s, want %s", tt.branch, rootString(got), rootString(*tt.want))
			}
		})
	}
}

func rootString(r model.Root) string {
	if r.Task != nil {
		return r.Task.Ref().String()
	}
	return ""
}
