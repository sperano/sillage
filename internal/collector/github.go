package collector

import (
	"context"
	"fmt"
	"maps"
	"net/http"
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
// GitHub is not read, and its coverage says why; nor is it while off, nor
// when the caller keeps its last read (Kept.SkipGitHub).
func (a *assembly) readGitHub(ctx context.Context, branch string, extra []github.Repository) {
	a.source(model.SourceGitHub)
	if !a.c.on(model.SourceGitHub) {
		return
	}
	if a.kept.SkipGitHub {
		a.keepGitHub()
		return
	}
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
	var all branchRead
	for _, repo := range repos {
		r, err := a.branchRecords(ctx, repo, branch)
		if err != nil {
			a.fail(model.SourceGitHub, err)
			return
		}
		all.add(r)
	}
	a.read(model.SourceGitHub, at)
	a.refusals = append(a.refusals, refusalsOf(a.source(model.SourceGitHub).cov.Instance, all.refused)...)
	a.addGitHub(all)
}

// addGitHub adds what the branches' reads hold to the story.
func (a *assembly) addGitHub(all branchRead) {
	a.addEvents(all.recs.Events)
	for _, p := range all.recs.PullRequests {
		a.prs[p.Key] = p
	}
	for _, c := range all.recs.Correlations {
		a.links.add(c)
	}
	for _, n := range all.notes {
		a.note(model.SourceGitHub, n)
	}
}

// branchRead is what was read of one branch's pull requests, or of
// several branches'.
type branchRead struct {
	recs model.Records
	// notes say what the read left unknown; refused are the sub-reads
	// GitHub refused.
	notes   []string
	refused []github.Refusal
}

// add appends r to b.
func (b *branchRead) add(r branchRead) {
	b.recs.Events = append(b.recs.Events, r.recs.Events...)
	b.recs.PullRequests = append(b.recs.PullRequests, r.recs.PullRequests...)
	b.recs.Correlations = append(b.recs.Correlations, r.recs.Correlations...)
	b.notes = append(b.notes, r.notes...)
	b.refused = append(b.refused, r.refused...)
}

// branchRecords reads and normalizes branch's pull requests in repo, each
// joined to the root by its head branch, and notes the commits whose
// checks were not read and the sub-reads GitHub refused.
func (a *assembly) branchRecords(ctx context.Context, repo github.Repository, branch string) (branchRead, error) {
	snaps, err := a.c.src.GitHub.FetchBranch(ctx, repo, github.Branch{Repository: repo, Name: branch})
	if err != nil {
		return branchRead{}, fmt.Errorf("%s branch %s: %w", repo, branch, err)
	}
	var b branchRead
	out := &b.recs
	ref := "GET /repos/" + repo.String() + "/pulls?state=all&head=" + repo.Owner + ":" + branch
	for _, s := range snaps {
		b.refused = append(b.refused, s.Refused...)
		recs, err := github.Normalize(s, a.cfg.GitHubAnnouncers)
		if err != nil {
			return branchRead{}, fmt.Errorf("%s#%d: %w", repo, s.PullRequest.Number, err)
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
				b.notes = append(b.notes, fmt.Sprintf("pull request %s: the checks of its %d oldest commits "+
					"were not read, so their results are unknown", p.Key, n))
			}
			b.notes = append(b.notes, refusalNotes(p.Key, s)...)
		}
	}
	return b, nil
}

// refusalKey is a resource GitHub refused and the status it answered.
type refusalKey struct {
	resource string
	code     int
}

// refusalsOf counts refused by resource and status, in the order first
// refused, as GitHub instance's.
func refusalsOf(instance string, refused []github.Refusal) []Refusal {
	var out []Refusal
	at := make(map[refusalKey]int)
	for _, r := range refused {
		k := refusalKey{r.Resource, r.Code}
		i, ok := at[k]
		if !ok {
			i = len(out)
			at[k] = i
			out = append(out, Refusal{
				Source: model.SourceGitHub, Instance: instance, Resource: r.Resource, Code: r.Code,
			})
		}
		out[i].Count++
		if r.Remembered {
			out[i].Remembered++
		}
	}
	return out
}

// refusalNotes says, for each resource and status GitHub refused in s,
// what of pull request key is unknown: "check runs: forbidden" for a token
// without the Checks permission.
func refusalNotes(key model.PRKey, s github.Snapshot) []string {
	var order []refusalKey
	commits := make(map[refusalKey]int)
	for _, r := range s.Refused {
		k := refusalKey{r.Resource, r.Code}
		if _, ok := commits[k]; !ok {
			order = append(order, k)
		}
		if r.SHA != "" {
			commits[k]++
		}
	}
	notes := make([]string, 0, len(order))
	for _, k := range order {
		answered, unknown := fmt.Sprintf("GitHub answered %d", k.code), "its inline review comments are unknown"
		if k.resource == github.ResourceCheckRuns {
			answered += fmt.Sprintf(" for %d of its %d commits", commits[k], len(s.SHAs))
			unknown = "those commits' checks are unknown"
		}
		notes = append(notes, fmt.Sprintf("pull request %s: %s: %s (%s), so %s",
			key, k.resource, strings.ToLower(http.StatusText(k.code)), answered, unknown))
	}
	return notes
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
