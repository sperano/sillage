package collector

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

// repository is a repository the story's branch is read in, and why: the
// evidence each pull request read there is joined to the root with,
// besides its head branch. A run's Origin, workspace or project label, or
// the caller, names a repository without evidence of its own.
type repository struct {
	github.Repository
	evidence []model.Evidence
}

// repositorySet gathers repositories, each once whatever its case, with
// the evidence of the first that named it.
type repositorySet struct {
	repos []repository
	seen  map[string]bool
}

func (s *repositorySet) add(repo github.Repository, evidence ...model.Evidence) {
	key := strings.ToLower(repo.String())
	if s.seen == nil {
		s.seen = make(map[string]bool)
	}
	if s.seen[key] {
		return
	}
	s.seen[key] = true
	s.repos = append(s.repos, repository{Repository: repo, evidence: evidence})
}

// sorted returns the repositories by name.
func (s *repositorySet) sorted() []repository {
	return slices.SortedFunc(slices.Values(s.repos), func(x, y repository) int {
		return strings.Compare(strings.ToLower(x.String()), strings.ToLower(y.String()))
	})
}

// repositories returns the repositories to read branch in, by name: those
// the Origins of the story's runs name, those the workspaces and projects
// of the others map to, extra, then those the story's other records name:
// a pull request link in the task's comments, a Task name's project, a
// pull request a store kept. It also returns the workspace and project
// names that map to none.
func (a *assembly) repositories(branch string, extra []github.Repository) ([]repository, []string) {
	var set repositorySet
	var unknown []string
	for _, r := range a.runs {
		if repo, ok := a.originRepository(r); ok {
			set.add(repo)
		}
	}
	for _, name := range a.workspaceNames() {
		if r, ok := a.cfg.Workspaces[name]; ok {
			set.add(r)
		} else if !slices.Contains(unknown, name) {
			unknown = append(unknown, name)
		}
	}
	for _, r := range extra {
		set.add(r)
	}
	a.linkedRepositories(&set)
	a.namedProjects(&set)
	a.keptRepositories(&set, branch)
	slices.Sort(unknown)
	return set.sorted(), unknown
}

// workspaceNames lists the workspace, else the project, of every run of
// the story whose Origin names no repository.
func (a *assembly) workspaceNames() []string {
	var names []string
	for _, r := range a.runs {
		if _, ok := a.originRepository(r); ok {
			continue
		}
		if ws := a.workspaceOf(r); ws != "" {
			names = append(names, ws)
		} else if r.Project != "" {
			names = append(names, r.Project)
		}
	}
	for _, e := range a.eventsOf(model.KindRunAnnounced) {
		if ws := e.Details[detailWorkspace]; ws != "" {
			names = append(names, ws)
		}
	}
	return names
}

// workspaceOf is the workspace label of r's Task, if Kelos lists it.
func (a *assembly) workspaceOf(r model.Run) string {
	if a.kelos == nil {
		return ""
	}
	return a.kelos.placements[r.Key].Workspace
}

// linkedRepositories adds the repositories of the pull requests the
// task's comments link to on the GitHub instance.
func (a *assembly) linkedRepositories(set *repositorySet) {
	for _, l := range a.prLinks {
		set.add(l.repo, l.evidence)
	}
}

// namedProjects adds the repositories that the projects of the story's
// announced Task names, and of its runs no label places, map to: the
// spawners name a task's Tasks vikunja-<project>-…, after the same
// project its runs' labels carry.
func (a *assembly) namedProjects(set *repositorySet) {
	id := a.root.Task.ID
	for _, e := range a.eventsOf(model.KindRunAnnounced) {
		name := e.Details[detailTaskName]
		if e.Details[detailWorkspace] != "" || name == "" {
			continue
		}
		for _, p := range projectsOfName(name, id, a.cfg.Workspaces) {
			repo := a.cfg.Workspaces[p]
			set.add(repo, a.announcementEvidence(e, "names Kelos Task "+name+", "+projectNote(p, repo)))
		}
	}
	for _, r := range a.runs {
		if _, ok := a.originRepository(r); ok || a.workspaceOf(r) != "" || r.Project != "" || len(r.Evidence) == 0 {
			continue
		}
		for _, p := range projectsOfName(r.TaskName, id, a.cfg.Workspaces) {
			repo, ev := a.cfg.Workspaces[p], r.Evidence[0]
			ev.Note = "Task name " + r.TaskName + ", " + projectNote(p, repo)
			set.add(repo, ev)
		}
	}
}

// projectNote says that a Task name names project p by convention, and
// where p's repository is.
func projectNote(p string, repo github.Repository) string {
	return "follows the spawners' convention for project " + p + ", which " + EnvWorkspaces + " maps to " +
		repo.String()
}

// keptRepositories adds the repositories of the pull requests a store
// kept of the story: an earlier read found them there.
func (a *assembly) keptRepositories(set *repositorySet, branch string) {
	for _, p := range a.kept.PullRequests {
		if !strings.EqualFold(p.Key.Instance, a.cfg.GitHubInstance) {
			continue
		}
		repo, err := github.ParseRepository(p.Key.Repository)
		if err != nil {
			continue
		}
		set.add(repo, model.Evidence{
			Source: model.SourceGitHub, Instance: a.cfg.GitHubInstance, Ref: pullsRef(repo, branch),
			Note: "the store holds pull request #" + strconv.Itoa(p.Key.Number) + " of this repository from an " +
				"earlier read of the story",
		})
	}
}

// searched returns every repository EnvWorkspaces maps to, for a story
// none of whose records names one: its branch is looked for in each.
func (a *assembly) searched(branch string) []repository {
	var set repositorySet
	for _, name := range slices.Sorted(maps.Keys(a.cfg.Workspaces)) {
		repo := a.cfg.Workspaces[name]
		set.add(repo, model.Evidence{
			Source: model.SourceGitHub, Instance: a.cfg.GitHubInstance, Ref: pullsRef(repo, branch),
			Note: "no record of the story names its repository, so branch " + branch +
				" was looked for in each repository " + EnvWorkspaces + " lists",
		})
	}
	return set.sorted()
}

// pullsRef is the request that lists branch's pull requests in repo.
func pullsRef(repo github.Repository, branch string) string {
	return "GET /repos/" + repo.String() + "/pulls?state=all&head=" + repo.Owner + ":" + branch
}
