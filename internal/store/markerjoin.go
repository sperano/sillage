package store

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sperano/sillage/internal/model"
)

const (
	// kelosTasksPrefix starts the evidence ref of a Kelos Task, the object
	// whose status names its run's pod.
	kelosTasksPrefix = "tasks/"
	// kelosPodsPrefix starts the evidence ref of the pod a marker's token
	// was verified for.
	kelosPodsPrefix = "pods/"
)

// runPod is a run seen with a pod: its Task UID, and the Kelos object that
// named the pod.
type runPod struct {
	uid, ref string
}

// pendingMarker is a stored marker not joined by its pod: unattached, or
// joined by name.
type pendingMarker struct {
	id                    int64
	pod, podUID, taskName string
	at                    time.Time
	// runUID and confidence are its join, nil while unattached.
	runUID, confidence *string
}

// joiner joins the markers of one namespace to their runs, in a write
// transaction. It reads each pod's and each Task name's runs once.
type joiner struct {
	ctx                context.Context
	tx                 pgx.Tx
	cluster, namespace string
	pods               map[string][]runPod
	named              map[string][]model.Run
}

func newJoiner(ctx context.Context, tx pgx.Tx, cluster, namespace string) *joiner {
	return &joiner{ctx: ctx, tx: tx, cluster: cluster, namespace: namespace,
		pods: make(map[string][]runPod), named: make(map[string][]model.Run)}
}

// rejoin joins again the namespace's markers that where matches (its
// parameters from $3) and their pod does not join yet, and stores each
// join that changed.
func (j *joiner) rejoin(where string, args ...any) error {
	rows, err := j.tx.Query(j.ctx, `SELECT id, pod, pod_uid, COALESCE(task_name, ''), at, run_uid, confidence
		FROM markers WHERE cluster = $1 AND namespace = $2 AND confidence IS DISTINCT FROM 'identifier'
		AND (`+where+`) ORDER BY id`, append([]any{j.cluster, j.namespace}, args...)...)
	if err != nil {
		return err
	}
	pending, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pendingMarker, error) {
		var p pendingMarker
		err := row.Scan(&p.id, &p.pod, &p.podUID, &p.taskName, &p.at, &p.runUID, &p.confidence)
		return p, err
	})
	if err != nil {
		return err
	}
	for _, p := range pending {
		join, err := j.joinOf(p)
		if err != nil {
			return err
		}
		if err := j.store(p, join); err != nil {
			return err
		}
	}
	return nil
}

// joinOf joins p by its pod, else by its Task name and time; nil when
// neither establishes one run.
func (j *joiner) joinOf(p pendingMarker) (*model.MarkerJoin, error) {
	pods, ok := j.pods[p.pod]
	if !ok {
		rows, err := j.tx.Query(j.ctx, `SELECT uid, ref FROM run_pods
			WHERE cluster = $1 AND namespace = $2 AND pod = $3 ORDER BY uid`, j.cluster, j.namespace, p.pod)
		if err != nil {
			return nil, err
		}
		if pods, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (runPod, error) {
			var r runPod
			return r, row.Scan(&r.uid, &r.ref)
		}); err != nil {
			return nil, err
		}
		j.pods[p.pod] = pods
	}
	if join := podJoin(j.cluster, j.namespace, p, pods); join != nil || p.taskName == "" {
		return join, nil
	}
	runs, ok := j.named[p.taskName]
	if !ok {
		var err error
		if runs, err = queryJSON[model.Run](j.ctx, j.tx, `SELECT body FROM runs
			WHERE cluster = $1 AND namespace = $2 AND uid IS NOT NULL AND body ->> 'taskName' = $3`,
			j.cluster, j.namespace, p.taskName); err != nil {
			return nil, err
		}
		j.named[p.taskName] = runs
	}
	return nameJoin(j.cluster, j.namespace, p, runs), nil
}

// store writes p's join unless it is the one stored.
func (j *joiner) store(p pendingMarker, join *model.MarkerJoin) error {
	switch {
	case join == nil && p.runUID == nil:
		return nil
	case join == nil:
		_, err := j.tx.Exec(j.ctx, `UPDATE markers SET run_uid = NULL, confidence = NULL, join_evidence = NULL
			WHERE id = $1`, p.id)
		return err
	case p.runUID != nil && *p.runUID == join.Run.UID && *p.confidence == string(join.Confidence):
		return nil
	}
	evidence, err := json.Marshal(join.Evidence)
	if err != nil {
		return err
	}
	_, err = j.tx.Exec(j.ctx, "UPDATE markers SET run_uid = $2, confidence = $3, join_evidence = $4 WHERE id = $1",
		p.id, join.Run.UID, string(join.Confidence), evidence)
	return err
}

// podJoin joins a marker of p's pod to the one run seen with that pod, as
// an identifier: the TaskReview verified the pod, and a Task's status
// named it. Two runs seen with one pod establish neither.
func podJoin(cluster, namespace string, p pendingMarker, pods []runPod) *model.MarkerJoin {
	if len(pods) != 1 {
		return nil
	}
	rp := pods[0]
	return &model.MarkerJoin{
		Run:        model.RunKey{Cluster: cluster, Namespace: namespace, UID: rp.uid},
		Confidence: model.ConfidenceIdentifier,
		Evidence: []model.Evidence{
			{Source: model.SourceKelos, Instance: cluster, Ref: rp.ref,
				Note: "pod " + p.pod + " named by " + rp.ref + " status.podName"},
			{Source: model.SourceKelos, Instance: cluster, Ref: kelosPodsPrefix + namespace + "/" + p.pod,
				Note: "marker token verified by a TokenReview for pod " + p.pod + " (uid " + p.podUID + ")"},
		},
	}
}

// nameJoin joins p to the one run among runs, of its Task name, whose
// interval holds its time, as a heuristic: the name is the pod's own
// claim, and a retry reuses it.
func nameJoin(cluster, namespace string, p pendingMarker, runs []model.Run) *model.MarkerJoin {
	var hit *model.Run
	for i := range runs {
		if !runHolds(runs[i], p.at) {
			continue
		}
		if hit != nil {
			return nil
		}
		hit = &runs[i]
	}
	if hit == nil {
		return nil
	}
	ref := taskEvidence(*hit)
	return &model.MarkerJoin{
		Run:        model.RunKey{Cluster: cluster, Namespace: namespace, UID: hit.Key.UID},
		Confidence: model.ConfidenceHeuristic,
		Evidence: []model.Evidence{{Source: model.SourceKelos, Instance: cluster, Ref: ref,
			Note: "no run is known for pod " + p.pod + "; the Task name the marker gives, " + p.taskName +
				", and its time, " + p.at.UTC().Format(time.RFC3339Nano) +
				", fit the interval of one run of that name only: uncertain"}},
	}
}

// runHolds reports whether at is inside r's interval, from its creation
// (else its start) to its end, or open: an unknown bound holds nothing.
func runHolds(r model.Run, at time.Time) bool {
	start, ok := r.Interval.Start.Time()
	if r.Created != nil {
		start, ok = *r.Created, true
	}
	if !ok || at.Before(start) {
		return false
	}
	switch r.Interval.End.State {
	case model.BoundOpen:
		return true
	case model.BoundKnown:
		end, _ := r.Interval.End.Time()
		return !at.After(end)
	}
	return false
}

// taskEvidence returns the ref of r's Kelos Task, else of its first
// evidence: where to look for the run.
func taskEvidence(r model.Run) string {
	for _, ev := range r.Evidence {
		if ev.Source == model.SourceKelos && strings.HasPrefix(ev.Ref, kelosTasksPrefix) {
			return ev.Ref
		}
	}
	// A stored run has evidence: model.BuildStory validates it before a save.
	return r.Evidence[0].Ref
}
