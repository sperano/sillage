package kelos

import (
	"math"
	"strings"

	"github.com/sperano/sillage/internal/model"
)

// maxPRNumber bounds a pull request number to what model.PRKey's int
// holds on every platform.
const maxPRNumber = math.MaxInt32

// taskLink returns the correlation with the Vikunja task the task label
// names, if any, on the configured Vikunja instance. A Vikunja Origin on
// that instance adds its evidence; one naming another host (the chart's
// view of the instance may differ from sillage's) does not move the link.
func (b *runBuilder) taskLink(labels map[string]string, ref string, origin Origin) []model.Correlation {
	id, ok := b.cfg.TaskID(labels)
	if !ok {
		return nil
	}
	key := b.cfg.label(labelTask)
	task := model.TaskKey{Instance: b.cfg.VikunjaInstance, ID: id}
	evidence := []model.Evidence{{
		Source:   model.SourceKelos,
		Instance: b.cfg.Cluster,
		Ref:      ref,
		Note:     "label " + key + "=" + labels[key],
	}}
	if host, ok := origin.instanceOf(OriginVikunja); ok && host == b.cfg.VikunjaInstance {
		evidence = append(evidence, origin.Evidence(b.cfg, AnnotationSource, AnnotationSourceInstance))
	}
	return []model.Correlation{identifier(b.run.Key.Ref(), task.Ref(), evidence...)}
}

// originLinks returns the correlations with the wiki page and the pull
// request the Origin names.
func (b *runBuilder) originLinks(o Origin) []model.Correlation {
	var out []model.Correlation
	from := b.run.Key.Ref()
	if page, ok := o.Page(); ok {
		names := []string{AnnotationSource, AnnotationSourceInstance, AnnotationWikiPage, AnnotationWikiLocale}
		if o.Value(AnnotationWikiPath) != "" {
			names = append(names, AnnotationWikiPath)
		}
		out = append(out, identifier(from, page.Ref(), o.Evidence(b.cfg, names...)))
	}
	if pr, ok := o.PullRequest(); ok {
		out = append(out, identifier(from, pr.Ref(), o.Evidence(b.cfg,
			AnnotationSource, AnnotationSourceInstance, AnnotationRepository, AnnotationPR)))
	}
	return out
}

func identifier(from, to model.Ref, evidence ...model.Evidence) model.Correlation {
	return model.Correlation{From: from, To: to, Confidence: model.ConfidenceIdentifier, Evidence: evidence}
}

// instanceOf returns the host of the source that started the Task, lower
// case as the model's keys name it, when that source is source.
func (o Origin) instanceOf(source string) (string, bool) {
	host := strings.ToLower(o.Value(AnnotationSourceInstance))
	return host, o.Value(AnnotationSource) == source && host != ""
}

// Page returns the wiki page a wiki follow-up's Origin names.
func (o Origin) Page() (model.PageKey, bool) {
	host, ok := o.instanceOf(OriginWiki)
	id, okID := o.ID(AnnotationWikiPage)
	locale := o.Value(AnnotationWikiLocale)
	if !ok || !okID || locale == "" {
		return model.PageKey{}, false
	}
	return model.PageKey{Instance: host, ID: id, Locale: locale}, true
}

// PullRequest returns the pull request a GitHub event's Origin names.
func (o Origin) PullRequest() (model.PRKey, bool) {
	host, ok := o.instanceOf(OriginGitHub)
	number, okNumber := o.ID(AnnotationPR)
	repo := o.Value(AnnotationRepository)
	if !ok || !okNumber || !validRepository(repo) || number > maxPRNumber {
		return model.PRKey{}, false
	}
	return model.PRKey{Instance: host, Repository: repo, Number: int(number)}, true
}

// validRepository reports whether s is owner/name.
func validRepository(s string) bool {
	owner, name, ok := strings.Cut(s, "/")
	return ok && owner != "" && name != "" && !strings.Contains(name, "/")
}
