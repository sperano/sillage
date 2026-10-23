package storetest

import (
	"time"

	"github.com/sperano/sillage/internal/model"
)

// The fabricated instances and records the suite commits.
const (
	vikunjaHost = "vikunja.example.com"
	cluster     = "cluster-a"
	namespace   = "agents"
	taskID      = 42
	taskName    = "vikunja-widgets-agent-opus-42"
	pod         = taskName + "-pod00"
	uid         = "uid-42"
)

// base is minute 0 of the suite's times.
var base = time.Date(2026, 3, 5, 12, 0, 0, 0, time.UTC)

func at(minute int) time.Time { return base.Add(time.Duration(minute) * time.Minute) }

func ptr[T any](v T) *T { return &v }

func taskKey() model.TaskKey { return model.TaskKey{Instance: vikunjaHost, ID: taskID} }

func root() model.Root { return model.Root{Task: ptr(taskKey())} }

func vikunjaEvidence() model.Evidence {
	return model.Evidence{Source: model.SourceVikunja, Instance: vikunjaHost, Ref: "GET /api/v1/tasks/42/comments"}
}

// comment is comment id on the task, posted at minute and read just
// after.
func comment(id string, minute int) model.Event {
	return model.Event{
		Source: model.SourceVikunja, Instance: vikunjaHost, ID: id, IDKind: model.IDSource,
		Kind: model.KindComment, SourceTime: ptr(at(minute)), ObservedAt: at(minute + 1),
		Actor:    model.Account{Login: "owner", Kind: model.ActorPerson},
		Related:  model.Related{Task: ptr(taskKey())},
		Evidence: vikunjaEvidence(),
	}
}

// placeholderRun is the task's run known only from its announcement,
// keyed by its pod.
func placeholderRun() model.Run {
	return model.Run{
		Key: model.RunKey{Cluster: cluster, Namespace: namespace, Pod: pod}, Pod: pod, TaskName: taskName,
		Role:     model.RoleImplementation,
		Interval: model.Interval{Start: model.UnknownBound(), End: model.UnknownBound()},
		Outcome:  model.OutcomeUnknown,
		Evidence: []model.Evidence{vikunjaEvidence()},
	}
}

// kelosRun is the task's run as Kelos read it, keyed by UID and naming
// the placeholder's pod.
func kelosRun() model.Run {
	return model.Run{
		Key: model.RunKey{Cluster: cluster, Namespace: namespace, UID: uid}, Pod: pod, TaskName: taskName,
		Agent: "agent-opus", Role: model.RoleImplementation,
		Interval: model.Interval{Start: model.KnownAt(at(1)), End: model.KnownAt(at(30))},
		Outcome:  model.OutcomeSucceeded,
		Evidence: []model.Evidence{{Source: model.SourceKelos, Instance: cluster, Ref: "tasks/agents/" + taskName}},
	}
}

// announces is the correlation from the announcement to run.
func announces(announcement model.Event, run model.Run) model.Correlation {
	return model.Correlation{
		From: announcement.Ref(), To: run.Key.Ref(), Confidence: model.ConfidenceIdentifier,
		Evidence: []model.Evidence{vikunjaEvidence()},
	}
}

// coverage is source's coverage read last at minute read, with gaps.
func coverage(source model.Source, instance string, read int, gaps ...model.Gap) model.Coverage {
	return model.Coverage{Source: source, Instance: instance, LastSuccessfulRead: ptr(at(read)), Gaps: gaps}
}

func gap(start, end int, reason string) model.Gap {
	return model.Gap{Start: at(start), End: at(end), Reason: reason}
}
