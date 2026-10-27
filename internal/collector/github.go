package collector

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
)

// taskBranchPrefix starts the branch Kelos works on for a Vikunja task.
const taskBranchPrefix = "kelos/vikunja-"

// taskBranch is the branch of Vikunja task id.
func taskBranch(id int64) string { return taskBranchPrefix + strconv.FormatInt(id, 10) }

// readGitHub reads the pull requests of branch, in every state, in each
// repository the story's runs name and in extra. Without a repository,
// GitHub is not read, and its coverage says why.
func (a *assembly) readGitHub(ctx context.Context, branch string, extra []github.Repository) {
	a.source(model.SourceGitHub)
	repos, unknown := a.repositories(extra)
	if len(unknown) > 0 {
		a.note(model.SourceGitHub, "no repository in "+EnvWorkspaces+" for "+strings.Join(unknown, ", ")+
			": the pull requests of their branch "+branch+" were not read")
	}
	if len(repos) == 0 {
		a.note(model.SourceGitHub, "no repository known for the story's branch "+branch+
			": GitHub was not read (name one with "+EnvWorkspaces+" or the repo flag)")
		return
	}
	at := a.c.now()
	var all model.Records
	var notes []string
	for _, repo := range repos {
		recs, unchecked, err := a.branchRecords(ctx, repo, branch)
		if err != nil {
			a.fail(model.SourceGitHub, err)
			return
		}
		all.Events = append(all.Events, recs.Events...)
		all.PullRequests = append(all.PullRequests, recs.PullRequests...)
		all.Correlations = append(all.Correlations, recs.Correlations...)
		notes = append(notes, unchecked...)
	}
	a.read(model.SourceGitHub, at)
	a.addEvents(all.Events)
	for _, p := range all.PullRequests {
		a.prs[p.Key] = p
	}
	for _, c := range all.Correlations {
		a.links.add(c)
	}
	for _, n := range notes {
		a.note(model.SourceGitHub, n)
	}
}

// branchRecords reads and normalizes branch's pull requests in repo, each
// joined to the root by its head branch, and notes the commits whose
// checks were not read.
func (a *assembly) branchRecords(ctx context.Context, repo github.Repository, branch string) (
	model.Records, []string, error) {
	snaps, err := a.c.src.GitHub.FetchBranch(ctx, repo, github.Branch{Repository: repo, Name: branch})
	if err != nil {
		return model.Records{}, nil, fmt.Errorf("%s branch %s: %w", repo, branch, err)
	}
	var out model.Records
	var notes []string
	ref := "GET /repos/" + repo.String() + "/pulls?state=all&head=" + repo.Owner + ":" + branch
	for _, s := range snaps {
		recs, err := github.Normalize(s, a.cfg.GitHubAnnouncers)
		if err != nil {
			return model.Records{}, nil, fmt.Errorf("%s#%d: %w", repo, s.PullRequest.Number, err)
		}
		out.Events = append(out.Events, recs.Events...)
		out.PullRequests = append(out.PullRequests, recs.PullRequests...)
		for _, p := range recs.PullRequests {
			out.Correlations = append(out.Correlations, model.Correlation{
				From: p.Key.Ref(), To: a.rootRef, Confidence: model.ConfidenceIdentifier,
				Evidence: []model.Evidence{{
					Source: model.SourceGitHub, Instance: p.Key.Instance, Ref: ref, Note: "head branch " + branch,
				}},
			})
			if n := len(s.UncheckedSHAs); n > 0 {
				notes = append(notes, fmt.Sprintf("pull request %s: the checks of its %d oldest commits "+
					"were not read, so their results are unknown", p.Key, n))
			}
		}
	}
	return out, notes, nil
}

// repositories returns the repositories to read, in order: those the
// Origins of the story's runs name, those the workspaces and projects of
// the others map to, and extra; and the names that map to none.
func (a *assembly) repositories(extra []github.Repository) ([]github.Repository, []string) {
	var repos []github.Repository
	var unknown []string
	for _, r := range a.runs {
		if repo, ok := a.originRepository(r); ok {
			repos = append(repos, repo)
		}
	}
	for _, name := range a.workspaceNames() {
		if r, ok := a.cfg.Workspaces[name]; ok {
			repos = append(repos, r)
		} else if !slices.Contains(unknown, name) {
			unknown = append(unknown, name)
		}
	}
	repos = append(repos, extra...)
	slices.SortFunc(repos, func(x, y github.Repository) int {
		return strings.Compare(strings.ToLower(x.String()), strings.ToLower(y.String()))
	})
	slices.Sort(unknown)
	return slices.CompactFunc(repos, func(x, y github.Repository) bool {
		return strings.EqualFold(x.String(), y.String())
	}), unknown
}

// followUpRoles are the roles of runs started on a pull request rather
// than on the task: they are joined to it by branch and Task name.
var followUpRoles = []model.RunRole{model.RolePRFollowUp, model.RoleConflictFix, model.RoleAutomation}

// prRuns joins the namespace's PR follow-up, conflict and bookkeeping
// runs, and the runs whose Origin names a pull request, to the story's
// pull requests. An Origin naming one of them is an identifier, and then
// the only join of its run. Otherwise the Task's branch is an identifier when
// it is the head of exactly one of them, or when the Task name picks one;
// a Task name alone (its Task gone) is a heuristic; a branch with several
// pull requests and no name to pick one makes each ambiguous.
func (a *assembly) prRuns() {
	if a.kelos == nil || len(a.prs) == 0 {
		return
	}
	prs := slices.SortedFunc(maps.Values(a.prs), func(x, y model.PullRequest) int {
		return strings.Compare(x.Key.String(), y.Key.String())
	})
	for _, r := range a.kelos.runs {
		if a.annotatedPRRun(r) {
			continue
		}
		if slices.Contains(followUpRoles, r.Role) {
			a.prRun(r, prs)
		}
	}
}

// annotatedPRRun joins r to the pull requests its Origin names that the
// story holds, and reports whether it named one. GitHub compares
// repository names without case.
func (a *assembly) annotatedPRRun(r model.Run) bool {
	joined := false
	for _, c := range a.annotatedLinks(r, model.RefPR) {
		for _, pr := range a.prs {
			if strings.EqualFold(pr.Key.Ref().ID, c.To.ID) {
				a.links.add(model.Correlation{
					From: c.From, To: pr.Key.Ref(), Confidence: c.Confidence, Evidence: c.Evidence,
				})
				a.addRun(r)
				joined = true
			}
		}
	}
	return joined
}

func (a *assembly) prRun(r model.Run, prs []model.PullRequest) {
	p := a.kelos.placements[r.Key]
	var byBranch, byName []model.PullRequest
	ws, number, named := prOfName(r.TaskName)
	for _, pr := range prs {
		if p.Branch != "" && pr.Branch == p.Branch && a.inWorkspace(p.Workspace, pr, true) {
			byBranch = append(byBranch, pr)
		}
		if named && pr.Key.Number == number && a.inWorkspace(ws, pr, false) {
			byName = append(byName, pr)
		}
	}
	if len(byName) == 1 {
		pr := byName[0]
		if slices.ContainsFunc(byBranch, func(b model.PullRequest) bool { return b.Key == pr.Key }) {
			a.links.identifier(r.Key.Ref(), pr.Key.Ref(), a.branchEvidence(p, headOf(pr)))
		}
		a.links.heuristic(r.Key.Ref(), pr.Key.Ref(), false, a.kelosEvidence(r, "Task name "+r.TaskName+
			" names pull request #"+strconv.Itoa(number)+" of workspace "+ws))
		a.addRun(r)
		return
	}
	for _, pr := range byBranch {
		if len(byBranch) == 1 {
			a.links.identifier(r.Key.Ref(), pr.Key.Ref(), a.branchEvidence(p, headOf(pr)))
		} else {
			a.links.heuristic(r.Key.Ref(), pr.Key.Ref(), true, a.branchEvidence(p, headOf(pr)))
		}
	}
	if len(byBranch) > 0 {
		a.addRun(r)
	}
}

// headOf says that a Task's branch is pr's head.
func headOf(pr model.PullRequest) string {
	return " is the head branch of pull request #" + strconv.Itoa(pr.Key.Number)
}

// inWorkspace reports whether pr is in workspace's repository. A
// workspace with no repository configured matches only when unknownOK.
func (a *assembly) inWorkspace(workspace string, pr model.PullRequest, unknownOK bool) bool {
	repo, ok := a.cfg.Workspaces[workspace]
	if !ok {
		return unknownOK
	}
	return strings.EqualFold(repo.String(), pr.Key.Repository)
}
