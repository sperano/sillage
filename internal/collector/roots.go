package collector

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/sperano/sillage/internal/model"
	"github.com/sperano/sillage/internal/source/kelos"
)

// Discovered is a root that a Kelos read names, with the runs that name
// it.
type Discovered struct {
	Root model.Root
	// Runs are the read's runs that name the root, in key order.
	Runs []model.Run
}

// Discover returns the roots the runs of one Kelos read name, in ref
// order: the Vikunja task of a run's task label, of its branch
// kelos/vikunja-<id>, or of its Task name by the task and comment
// spawners' conventions. A run can name several roots, and is listed with
// each.
func (c *Collector) Discover(snap kelos.Snapshot) ([]Discovered, error) {
	recs, err := kelos.Normalize(c.cfg.Kelos, snap)
	if err != nil {
		return nil, err
	}
	labelled := c.labelledTasks(snap)
	placements := kelos.Placements(c.cfg.Kelos, snap)
	found := make(map[model.Ref]*Discovered)
	for _, r := range recs.Runs {
		for _, root := range c.rootsOfRun(r, labelled[r.Key], placements[r.Key].Branch) {
			d, ok := found[root.Ref()]
			if !ok {
				d = &Discovered{Root: root}
				found[root.Ref()] = d
			}
			d.Runs = append(d.Runs, r)
		}
	}
	out := make([]Discovered, 0, len(found))
	for _, ref := range slices.SortedFunc(maps.Keys(found), compareRefs) {
		d := found[ref]
		slices.SortFunc(d.Runs, func(x, y model.Run) int { return strings.Compare(x.Key.String(), y.Key.String()) })
		out = append(out, *d)
	}
	return out, nil
}

// labelledTasks returns the task id each run's task label names, from
// its Task or, once the Task is gone, the TaskRecord's copy of its
// labels, as kelos.Normalize reads them: a Task's labels win whenever it
// exists, even without a task label, over a record's stale copy.
func (c *Collector) labelledTasks(snap kelos.Snapshot) map[model.RunKey]int64 {
	labels := make(map[string]map[string]string)
	for _, r := range snap.Records {
		labels[string(r.Spec.TaskRef.UID)] = r.Labels
	}
	for _, t := range snap.Tasks {
		labels[string(t.UID)] = t.Labels
	}
	out := make(map[model.RunKey]int64)
	for uid, l := range labels {
		if id, ok := c.cfg.Kelos.TaskID(l); ok && uid != "" {
			out[model.RunKey{Cluster: c.cfg.Kelos.Cluster, Namespace: c.cfg.Kelos.Namespace, UID: uid}] = id
		}
	}
	return out
}

// rootsOfRun returns the roots one run names, each once: by its task
// label (labelled, 0 for none), its branch and its Task name.
func (c *Collector) rootsOfRun(r model.Run, labelled int64, branch string) []model.Root {
	tasks := make(map[int64]bool)
	if labelled > 0 {
		tasks[labelled] = true
	}
	if id, ok := branchID(branch, taskBranch); ok {
		tasks[id] = true
	}
	if id, _, ok := taskOfName(r.TaskName); ok {
		tasks[id] = true
	}
	var out []model.Root
	for _, id := range slices.Sorted(maps.Keys(tasks)) {
		out = append(out, model.Root{Task: &model.TaskKey{Instance: c.cfg.VikunjaInstance, ID: id}})
	}
	return out
}

// branchID returns the id that branch names, when it is the branch of
// that id: it ends in the id, after a dash.
func branchID(branch string, of func(id int64) string) (int64, bool) {
	id, ok := parseID(branch[strings.LastIndexByte(branch, '-')+1:])
	if !ok || id <= 0 || of(id) != branch {
		return 0, false
	}
	return id, true
}

func compareRefs(x, y model.Ref) int {
	return cmp.Or(cmp.Compare(x.Kind, y.Kind), cmp.Compare(x.ID, y.ID))
}
