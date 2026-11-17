package collector

import (
	"regexp"
	"strconv"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/github"
	"github.com/sperano/sillage/internal/source/vikunja"
)

// prLink is a link to a pull request on the GitHub instance in one of the
// task's comments, and the evidence that the comment holds it.
type prLink struct {
	repo     github.Repository
	evidence model.Evidence
}

// prLinkPattern matches the web address of a pull request on host, its
// owner, repository and number captured: as the PR-open automation and
// people post them, in a link's target or its text.
func prLinkPattern(host string) *regexp.Regexp {
	const name = `([A-Za-z0-9_.-]+)`
	return regexp.MustCompile(`https?://(?i:` + regexp.QuoteMeta(host) + `)/` + name + `/` + name +
		`/pull/([1-9][0-9]*)`)
}

// pullRequestLinks returns the links to pull requests on the GitHub
// instance in s's comments, events the comments' normalized events: each
// repository's evidence is the comment it is linked from.
func (a *assembly) pullRequestLinks(s vikunja.Snapshot, events []model.Event) []prLink {
	byObject := make(map[string]model.Event, len(events))
	for _, e := range events {
		byObject[e.ID] = e
	}
	var out []prLink
	for _, c := range s.Comments {
		e, ok := byObject[commentObject(c.ID)]
		if !ok {
			continue
		}
		for _, m := range a.c.prLink.FindAllStringSubmatch(c.Body, -1) {
			repo, err := github.ParseRepository(m[1] + "/" + m[2])
			if err != nil {
				continue
			}
			out = append(out, prLink{repo: repo, evidence: model.Evidence{
				Source: e.Evidence.Source, Instance: e.Evidence.Instance, Ref: e.Evidence.Ref,
				Note: "comment " + strconv.FormatInt(c.ID, 10) + " links pull request #" + m[3] + " of " + repo.String(),
			}})
		}
	}
	return out
}
