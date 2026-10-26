package ingest

import "github.com/sperano/sillage/internal/model"

// rootSet collects roots, each once, in the order first added.
type rootSet struct {
	roots []model.Root
	seen  map[model.Ref]bool
}

func (s *rootSet) add(root model.Root) {
	ref, ok := refOf(root)
	if !ok || s.seen[ref] {
		return
	}
	if s.seen == nil {
		s.seen = make(map[model.Ref]bool)
	}
	s.seen[ref] = true
	s.roots = append(s.roots, root)
}

// refOf is the reference of a valid root's key.
func refOf(root model.Root) (model.Ref, bool) {
	if root.Validate() != nil {
		return model.Ref{}, false
	}
	return root.Task.Ref(), true
}
