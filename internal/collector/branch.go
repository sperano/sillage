package collector

import (
	"strconv"
	"strings"

	"github.com/sperano/sillage/internal/model"
)

// branchPageLocale is the locale of a page a branch names: the wiki
// spawner names a page by its id alone, which Wiki.js keeps unique across
// locales, and the agents' wiki is in this locale.
const branchPageLocale = "en"

// RootOfBranch returns the root whose work a branch carries: the Vikunja
// task of kelos/vikunja-<id>, or the wiki page of kelos/wiki-<id> (in
// branchPageLocale). Any other branch names no root.
func (c Config) RootOfBranch(branch string) (model.Root, bool) {
	if id, ok := idOfBranch(branch, taskBranch); ok {
		return model.Root{Task: &model.TaskKey{Instance: c.VikunjaInstance, ID: id}}, true
	}
	if id, ok := idOfBranch(branch, pageBranch); ok {
		return model.Root{Page: &model.PageKey{Instance: c.WikiInstance, ID: id, Locale: branchPageLocale}}, true
	}
	return model.Root{}, false
}

// idOfBranch returns the id branch ends in, after its last dash, when of
// gives that id this very branch: no other prefix, no leading zero.
func idOfBranch(branch string, of func(id int64) string) (int64, bool) {
	i := strings.LastIndexByte(branch, '-')
	if i < 0 {
		return 0, false
	}
	id, err := strconv.ParseInt(branch[i+1:], 10, 64)
	if err != nil || id <= 0 || of(id) != branch {
		return 0, false
	}
	return id, true
}
