package store

import (
	"cmp"
	"maps"
	"slices"

	"github.com/sperano/sillage/internal/model"
)

// podDetail is the run.observed detail naming the Task's pod at the time.
const podDetail = "pod"

// namespaceKey is one namespace of one cluster.
type namespaceKey struct{ cluster, namespace string }

// seenPods is what a batch says of the runs of one namespace: each run's
// pods, and the Task names of its runs.
type seenPods struct {
	pairs map[runPodKey]string
	names map[string]bool
}

// runPodKey is one pod seen with one run.
type runPodKey struct{ uid, pod string }

// joinMarkers keeps every pod the batch saw with a run keyed by Task UID
// (each run's pod, and the pod each run.observed event names), then joins
// again the markers of those pods and Task names that no pod joined yet:
// a marker that arrived before its run joins once the run is read.
func (w *saver) joinMarkers(story model.Story) error {
	seen := podsOf(story)
	for _, ns := range slices.SortedFunc(maps.Keys(seen), compareNamespaces) {
		s := seen[ns]
		for _, k := range slices.SortedFunc(maps.Keys(s.pairs), compareRunPods) {
			if err := w.exec(`INSERT INTO run_pods (cluster, namespace, uid, pod, ref) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT DO NOTHING`, ns.cluster, ns.namespace, k.uid, k.pod, s.pairs[k]); err != nil {
				return err
			}
		}
		pods := make([]string, 0, len(s.pairs))
		for k := range s.pairs {
			pods = append(pods, k.pod)
		}
		names := slices.Sorted(maps.Keys(s.names))
		j := newJoiner(w.ctx, w.tx, ns.cluster, ns.namespace)
		if err := j.rejoin("pod = ANY($3) OR task_name = ANY($4)", pods, names); err != nil {
			return err
		}
	}
	return nil
}

// podsOf collects the pods and Task names of the story's runs keyed by
// Task UID, by namespace.
func podsOf(story model.Story) map[namespaceKey]*seenPods {
	out := make(map[namespaceKey]*seenPods)
	of := func(k model.RunKey) *seenPods {
		ns := namespaceKey{k.Cluster, k.Namespace}
		if out[ns] == nil {
			out[ns] = &seenPods{pairs: make(map[runPodKey]string), names: make(map[string]bool)}
		}
		return out[ns]
	}
	for _, r := range story.Runs {
		if r.Key.UID == "" {
			continue
		}
		s := of(r.Key)
		if r.TaskName != "" {
			s.names[r.TaskName] = true
		}
		if r.Pod != "" {
			s.pairs[runPodKey{r.Key.UID, r.Pod}] = taskEvidence(r)
		}
	}
	for _, e := range story.Events {
		run, pod := e.Related.Run, e.Details[podDetail]
		if e.Kind != model.KindRunObserved || run == nil || run.UID == "" || pod == "" {
			continue
		}
		s := of(*run)
		if _, ok := s.pairs[runPodKey{run.UID, pod}]; !ok {
			s.pairs[runPodKey{run.UID, pod}] = e.Evidence.Ref
		}
	}
	return out
}

func compareNamespaces(a, b namespaceKey) int {
	return cmp.Or(cmp.Compare(a.cluster, b.cluster), cmp.Compare(a.namespace, b.namespace))
}

func compareRunPods(a, b runPodKey) int {
	return cmp.Or(cmp.Compare(a.uid, b.uid), cmp.Compare(a.pod, b.pod))
}
