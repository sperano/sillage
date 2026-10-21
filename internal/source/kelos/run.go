package kelos

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/sperano/sillage/internal/model"
)

const (
	// lifecycleNote labels the times a run takes from an object.
	lifecycleNote = "times are lifecycle times (the API server's creation, the controller's start and " +
		"completion), not CPU or phase timings"
	// noTaskNote explains a run read from its TaskRecord alone.
	noTaskNote = "Task not found (deleted after its TTL): created, pod, job and message unknown"
	// noRecordNote explains a run read from its Task alone.
	noRecordNote = "no TaskRecord found (Kelos writes one only for a completed Task that reported usage)"
)

// runBuilder fills a run field by field from an attempt's objects, noting
// which object supplied each field.
type runBuilder struct {
	cfg          Config
	a            *attempt
	run          model.Run
	taskFields   []string
	recordFields []string
	// timesFromTask and timesFromRecord note which objects supplied a
	// time.
	timesFromTask, timesFromRecord bool
}

// build returns the attempt's run and, when its labels name a Vikunja
// task, its correlation with that task. Fields only a Task holds (creation,
// pod, job, message) come from the Task; the terminal interval, outcome,
// model and usage come from the TaskRecord when there is one, else from
// the Task; labels come from the Task, else the TaskRecord's copy.
func (c Config) build(a *attempt) (model.Run, *model.Correlation) {
	b := &runBuilder{cfg: c, a: a}
	b.run.Key = model.RunKey{Cluster: c.Cluster, Namespace: c.Namespace, UID: string(a.uid)}
	b.name()
	b.taskOnly()
	link := b.labels()
	b.lifecycle()
	b.model()
	b.usage()
	b.run.Evidence = b.evidence()
	return b.run, link
}

func (b *runBuilder) fromTask(field, path string) {
	b.taskFields = append(b.taskFields, field+" ("+path+")")
}

func (b *runBuilder) fromRecord(field, path string) {
	b.recordFields = append(b.recordFields, field+" ("+path+")")
}

func (b *runBuilder) name() {
	if t := b.a.task; t != nil {
		b.run.TaskName = t.Name
		b.fromTask("taskName", "metadata.name")
		return
	}
	b.run.TaskName = b.a.record.Spec.TaskRef.Name
	b.fromRecord("taskName", "spec.taskRef.name")
}

func (b *runBuilder) taskOnly() {
	t := b.a.task
	if t == nil {
		return
	}
	if !t.CreationTimestamp.IsZero() {
		created := t.CreationTimestamp.Time
		b.run.Created = &created
		b.timesFromTask = true
		b.fromTask("created", "metadata.creationTimestamp")
	}
	b.taskString(&b.run.Pod, t.Status.PodName, "pod", "status.podName")
	b.taskString(&b.run.Job, t.Status.JobName, "job", "status.jobName")
	b.taskString(&b.run.Message, t.Status.Message, "message", "status.message")
}

func (b *runBuilder) taskString(dst *string, value, field, path string) {
	if value != "" {
		*dst = value
		b.fromTask(field, path)
	}
}

// labels sets the agent, project and role from the Task's labels, else
// the TaskRecord's copy, and returns the correlation with the Vikunja task
// the task label names, if any.
func (b *runBuilder) labels() *model.Correlation {
	labels, from, ref := b.labelSource()
	labelValue := func(dst *string, name, field string) {
		key := b.cfg.label(name)
		if v := labels[key]; v != "" {
			*dst = v
			from(field, labelPath(key))
		}
	}
	labelValue(&b.run.Agent, labelVariant, "agent")
	labelValue(&b.run.Project, labelProject, "project")
	role, why := b.cfg.roleOf(labels, b.run.TaskName)
	b.run.Role = role
	if why != "" {
		from("role", why)
	}
	return b.taskLink(labels, ref)
}

// labelSource returns the labels to read, how to note a field taken from
// them, and their object's ref.
func (b *runBuilder) labelSource() (map[string]string, func(field, path string), string) {
	if t := b.a.task; t != nil {
		return t.Labels, b.fromTask, b.cfg.taskRef(t.Name)
	}
	r := b.a.record
	return r.Labels, b.fromRecord, b.cfg.recordRef(r.Name)
}

func (b *runBuilder) taskLink(labels map[string]string, ref string) *model.Correlation {
	id, ok := b.cfg.taskID(labels)
	if !ok {
		return nil
	}
	key := b.cfg.label(labelTask)
	task := model.TaskKey{Instance: b.cfg.VikunjaInstance, ID: id}
	return &model.Correlation{
		From:       b.run.Key.Ref(),
		To:         task.Ref(),
		Confidence: model.ConfidenceIdentifier,
		Evidence: []model.Evidence{{
			Source:   model.SourceKelos,
			Instance: b.cfg.Cluster,
			Ref:      ref,
			Note:     "label " + key + "=" + labels[key],
		}},
	}
}

// lifecycle sets the interval and outcome from the TaskRecord, which only
// a completed Task has, else from the Task's status.
func (b *runBuilder) lifecycle() {
	if r := b.a.record; r != nil {
		s := r.Spec
		b.timesFromRecord = b.setLifecycle(s.Phase, s.StartTime, s.CompletionTime, "spec.", b.fromRecord)
		return
	}
	s := b.a.task.Status
	if b.setLifecycle(s.Phase, s.StartTime, s.CompletionTime, "status.", b.fromTask) {
		b.timesFromTask = true
	}
}

// setLifecycle sets the interval and outcome from a phase and its times,
// and reports whether it took a time. A phase not yet terminal leaves the
// end open; a phase it does not know makes the outcome unknown.
func (b *runBuilder) setLifecycle(phase Phase, start, end *metav1.Time, prefix string, from func(string, string)) bool {
	iv := model.Interval{Start: bound(start), End: bound(end)}
	outcome := model.OutcomeUnknown
	switch phase {
	case PhaseSucceeded:
		outcome = model.OutcomeSucceeded
	case PhaseFailed:
		outcome = model.OutcomeFailed
	case "", PhasePending, PhaseWaiting, PhaseRunning:
		outcome, iv.End = model.OutcomeRunning, model.OpenEnd()
	}
	b.run.Interval, b.run.Outcome = iv, outcome
	from("outcome", prefix+"phase "+string(phase))
	if iv.Start.State == model.BoundKnown {
		from("start", prefix+"startTime")
	}
	if iv.End.State == model.BoundKnown {
		from("end", prefix+"completionTime")
	}
	return iv.Start.State == model.BoundKnown || iv.End.State == model.BoundKnown
}

func bound(t *metav1.Time) model.Bound {
	if t == nil || t.IsZero() {
		return model.UnknownBound()
	}
	return model.KnownAt(t.Time)
}

// model sets the effective model: the TaskRecord's, else the Task's own
// unless a WorkerPool, which is not read, sets it.
func (b *runBuilder) model() {
	if r := b.a.record; r != nil && r.Spec.Model != "" {
		b.run.Model = r.Spec.Model
		b.fromRecord("model", "spec.model")
		return
	}
	t := b.a.task
	if t == nil || t.Spec.WorkerPoolRef != nil {
		return
	}
	if w := t.Spec.Worker; w != nil && w.Model != "" {
		b.taskString(&b.run.Model, w.Model, "model", "spec.worker.model")
		return
	}
	b.taskString(&b.run.Model, t.Spec.Model, "model", "spec.model")
}

func (b *runBuilder) usage() {
	if r := b.a.record; r != nil && r.Spec.Usage != nil {
		b.run.Usage = usageOf(r.Spec.Usage)
		b.fromRecord("usage", "spec.usage")
		return
	}
	if t := b.a.task; t != nil && t.Status.Usage != nil {
		b.run.Usage = usageOf(t.Status.Usage)
		b.fromTask("usage", "status.usage")
	}
}

func usageOf(u *TaskUsage) *model.Usage {
	out := &model.Usage{InputTokens: cloneInt(u.InputTokens), OutputTokens: cloneInt(u.OutputTokens)}
	if u.CostUSD != nil {
		out.CostUSD = u.CostUSD.AsDec().String()
	}
	return out
}

func cloneInt(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// evidence names each object read, the uid and the fields it supplied.
func (b *runBuilder) evidence() []model.Evidence {
	var out []model.Evidence
	if t := b.a.task; t != nil {
		out = append(out, b.evidenceOf(b.cfg.taskRef(t.Name), b.taskFields, b.timesFromTask, b.a.record == nil, noRecordNote))
	}
	if r := b.a.record; r != nil {
		out = append(out, b.evidenceOf(b.cfg.recordRef(r.Name), b.recordFields, b.timesFromRecord, b.a.task == nil, noTaskNote))
	}
	return out
}

func (b *runBuilder) evidenceOf(ref string, fields []string, times, alone bool, aloneNote string) model.Evidence {
	parts := []string{"uid " + string(b.a.uid) + " supplies " + strings.Join(fields, ", ")}
	if times {
		parts = append(parts, lifecycleNote)
	}
	if alone {
		parts = append(parts, aloneNote)
	}
	return model.Evidence{Source: model.SourceKelos, Instance: b.cfg.Cluster, Ref: ref, Note: strings.Join(parts, "; ")}
}
