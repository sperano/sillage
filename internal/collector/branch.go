package collector

import "github.com/sperano/sillage/internal/model"

// RootOfBranch returns the root whose work a branch carries: the Vikunja
// task of kelos/vikunja-<id>. Any other branch names no root.
func (c Config) RootOfBranch(branch string) (model.Root, bool) {
	if id, ok := branchID(branch, taskBranch); ok {
		return model.Root{Task: &model.TaskKey{Instance: c.VikunjaInstance, ID: id}}, true
	}
	return model.Root{}, false
}
