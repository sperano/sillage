package model

import (
	"errors"
	"fmt"
	"strconv"
)

// Source is the kind of system a record comes from. One source can have
// several instances (two Vikunja servers, two clusters).
type Source string

// The sources sillage reads.
const (
	SourceVikunja Source = "vikunja"
	SourceKelos   Source = "kelos"
	SourceGitHub  Source = "github"
	SourceWiki    Source = "wiki"
)

// Valid reports whether s is one of the sources sillage reads.
func (s Source) Valid() bool {
	switch s {
	case SourceVikunja, SourceKelos, SourceGitHub, SourceWiki:
		return true
	}
	return false
}

// TaskKey identifies a Vikunja task: the instance's host and the task's
// numeric id (not its per-project index).
type TaskKey struct {
	Instance string `json:"instance"`
	ID       int64  `json:"id"`
}

func (k TaskKey) String() string {
	return k.Instance + "/tasks/" + strconv.FormatInt(k.ID, 10)
}

// Validate reports whether every part of the key is set.
func (k TaskKey) Validate() error {
	if k.Instance == "" || k.ID <= 0 {
		return fmt.Errorf("task key %q: instance and a positive id are required", k)
	}
	return nil
}

// PageKey identifies a Wiki.js page: the instance's host, the page id and
// the locale. The path is not part of the key: a page can be moved.
type PageKey struct {
	Instance string `json:"instance"`
	ID       int64  `json:"id"`
	Locale   string `json:"locale"`
}

func (k PageKey) String() string {
	return k.Instance + "/pages/" + strconv.FormatInt(k.ID, 10) + "/" + k.Locale
}

// Validate reports whether every part of the key is set.
func (k PageKey) Validate() error {
	if k.Instance == "" || k.ID <= 0 || k.Locale == "" {
		return fmt.Errorf("page key %q: instance, a positive id and locale are required", k)
	}
	return nil
}

// RunKey identifies one run: one Kelos Task attempt. Its identity is the
// Task UID within a cluster and namespace, never the Task name or the
// branch, which a retry reuses.
//
// When no Kelos object of the run was ever read (the Task and its
// TaskRecord were gone before the collector looked) and the only evidence
// is a start announcement, which names the pod but not the UID, the pod
// name stands in for the UID: a pod belongs to exactly one attempt. Exactly
// one of UID and Pod is set. Once the UID is read, the collector re-keys the
// run by UID: a story never holds both keys for one pod. An announcement
// names neither cluster nor namespace; the collector takes them from the
// configuration of the Kelos instance the announcement links to.
type RunKey struct {
	Cluster   string `json:"cluster"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid,omitempty"`
	Pod       string `json:"pod,omitempty"`
}

func (k RunKey) String() string {
	if k.UID != "" {
		return k.Cluster + "/" + k.Namespace + "/uid/" + k.UID
	}
	return k.Cluster + "/" + k.Namespace + "/pod/" + k.Pod
}

// Validate reports whether the key names a cluster, a namespace and
// exactly one of UID and Pod.
func (k RunKey) Validate() error {
	if k.Cluster == "" || k.Namespace == "" {
		return fmt.Errorf("run key %q: cluster and namespace are required", k)
	}
	if (k.UID == "") == (k.Pod == "") {
		return fmt.Errorf("run key %q: exactly one of uid and pod is required", k)
	}
	return nil
}

// PRKey identifies a GitHub pull request: the instance's host, the
// repository as owner/name, and the number.
type PRKey struct {
	Instance   string `json:"instance"`
	Repository string `json:"repository"`
	Number     int    `json:"number"`
}

func (k PRKey) String() string {
	return k.Instance + "/" + k.Repository + "/pull/" + strconv.Itoa(k.Number)
}

// Validate reports whether every part of the key is set.
func (k PRKey) Validate() error {
	if k.Instance == "" || k.Repository == "" || k.Number <= 0 {
		return fmt.Errorf("pull request key %q: instance, repository and a positive number are required", k)
	}
	return nil
}

// Root is what a story is about: a Vikunja task, or a wiki page when the
// work was requested from the wiki and has no Vikunja task. Exactly one of
// Task and Page is set.
type Root struct {
	Task *TaskKey `json:"task,omitempty"`
	Page *PageKey `json:"page,omitempty"`
}

// Validate reports whether exactly one valid key is set.
func (r Root) Validate() error {
	switch {
	case r.Task != nil && r.Page != nil:
		return errors.New("root: both a task and a page are set")
	case r.Task != nil:
		return r.Task.Validate()
	case r.Page != nil:
		return r.Page.Validate()
	}
	return errors.New("root: neither a task nor a page is set")
}

// Ref returns a reference to the root's task or page; the zero Ref when
// neither is set.
func (r Root) Ref() Ref {
	switch {
	case r.Task != nil:
		return r.Task.Ref()
	case r.Page != nil:
		return r.Page.Ref()
	}
	return Ref{}
}

// RefKind is the kind of record a Ref points to.
type RefKind string

// The kinds of record a correlation can link.
const (
	RefTask  RefKind = "task"
	RefPage  RefKind = "page"
	RefRun   RefKind = "run"
	RefPR    RefKind = "pr"
	RefEvent RefKind = "event"
)

// Ref points to a task, page, run, pull request or event by its key's
// String form.
type Ref struct {
	Kind RefKind `json:"kind"`
	ID   string  `json:"id"`
}

func (r Ref) String() string { return string(r.Kind) + ":" + r.ID }

// Validate reports whether the kind is known and the id set.
func (r Ref) Validate() error {
	switch r.Kind {
	case RefTask, RefPage, RefRun, RefPR, RefEvent:
	default:
		return fmt.Errorf("ref %q: unknown kind", r)
	}
	if r.ID == "" {
		return fmt.Errorf("ref %q: id is required", r)
	}
	return nil
}

// Ref returns a reference to the task.
func (k TaskKey) Ref() Ref { return Ref{Kind: RefTask, ID: k.String()} }

// Ref returns a reference to the page.
func (k PageKey) Ref() Ref { return Ref{Kind: RefPage, ID: k.String()} }

// Ref returns a reference to the run.
func (k RunKey) Ref() Ref { return Ref{Kind: RefRun, ID: k.String()} }

// Ref returns a reference to the pull request.
func (k PRKey) Ref() Ref { return Ref{Kind: RefPR, ID: k.String()} }
