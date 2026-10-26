package model

// Refs returns every record a correlation among r, in root's story, may
// point to: root, the events and the tasks and pages they name, the runs
// and the pull requests.
func (r Records) Refs(root Root) map[Ref]bool {
	known := make(map[Ref]bool)
	addRelated := func(rel Related) {
		if rel.Task != nil {
			known[rel.Task.Ref()] = true
		}
	}
	addRelated(Related{Task: root.Task})
	for _, e := range r.Events {
		known[e.Ref()] = true
		addRelated(e.Related)
	}
	for _, run := range r.Runs {
		known[run.Key.Ref()] = true
	}
	for _, p := range r.PullRequests {
		known[p.Key.Ref()] = true
	}
	return known
}
