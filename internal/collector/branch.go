package collector

import "github.com/sperano/sillage/internal/model"

// RootOfBranch returns the root whose work a branch carries: the Vikunja
// task of kelos/vikunja-<id>, or the wiki page of kelos/wiki-<id> (in
// DiscoveredLocale). Any other branch names no root.
func (c Config) RootOfBranch(branch string) (model.Root, bool) {
	if id, ok := branchID(branch, taskBranch); ok {
		return model.Root{Task: &model.TaskKey{Instance: c.VikunjaInstance, ID: id}}, true
	}
	if id, ok := branchID(branch, pageBranch); ok {
		return model.Root{Page: &model.PageKey{Instance: c.WikiInstance, ID: id, Locale: DiscoveredLocale}}, true
	}
	return model.Root{}, false
}
