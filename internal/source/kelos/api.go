package kelos

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// The fields of Kelos 0.60.0's kelos.dev/v1alpha2 objects that sillage
// reads. Anything else, the prompt above all, is dropped when an object is
// decoded.

const (
	// apiGroup and apiVersion are Kelos's API group and version.
	apiGroup   = "kelos.dev"
	apiVersion = "v1alpha2"
	// tasksResource and recordsResource name the two resources read.
	tasksResource   = "tasks"
	recordsResource = "taskrecords"
)

var (
	taskGVR   = schema.GroupVersionResource{Group: apiGroup, Version: apiVersion, Resource: tasksResource}
	recordGVR = schema.GroupVersionResource{Group: apiGroup, Version: apiVersion, Resource: recordsResource}
)

// Phase is a Task's phase.
type Phase string

// The phases of a Task.
const (
	PhasePending   Phase = "Pending"
	PhaseRunning   Phase = "Running"
	PhaseSucceeded Phase = "Succeeded"
	PhaseFailed    Phase = "Failed"
	// PhaseWaiting: waiting for a dependency, a branch lock or a budget.
	PhaseWaiting Phase = "Waiting"
)

// ongoing reports whether p is a phase a Task passes through before it
// ends; the empty phase is a Task the controller has not reported on yet.
func (p Phase) ongoing() bool {
	switch p {
	case "", PhasePending, PhaseWaiting, PhaseRunning:
		return true
	}
	return false
}

// Task is a Kelos Task: one attempt, with its own UID.
type Task struct {
	metav1.ObjectMeta `json:"metadata"`
	Spec              TaskSpec   `json:"spec"`
	Status            TaskStatus `json:"status"`
}

// TaskSpec holds where the Task's model is set.
type TaskSpec struct {
	Worker *WorkerSpec `json:"worker,omitempty"`
	// WorkerPoolRef, when set, makes the pool's worker the effective one.
	WorkerPoolRef *WorkerPoolReference `json:"workerPoolRef,omitempty"`
	// Model is the deprecated spec.model, used when spec.worker.model is
	// unset.
	Model string `json:"model,omitempty"`
}

// WorkerSpec is a Task's worker.
type WorkerSpec struct {
	Model string `json:"model,omitempty"`
}

// WorkerPoolReference names a WorkerPool.
type WorkerPoolReference struct {
	Name string `json:"name"`
}

// TaskStatus is what the controller last recorded about the Task.
type TaskStatus struct {
	Phase          Phase        `json:"phase,omitempty"`
	JobName        string       `json:"jobName,omitempty"`
	PodName        string       `json:"podName,omitempty"`
	StartTime      *metav1.Time `json:"startTime,omitempty"`
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	Message        string       `json:"message,omitempty"`
	Usage          *TaskUsage   `json:"usage,omitempty"`
}

// TaskUsage is the usage a Task reported.
type TaskUsage struct {
	CostUSD      *resource.Quantity `json:"costUSD,omitempty"`
	InputTokens  *int64             `json:"inputTokens,omitempty"`
	OutputTokens *int64             `json:"outputTokens,omitempty"`
}

// TaskRecord is the immutable record Kelos keeps of a completed Task that
// reported usage. Kelos names it after the Task's UID and copies the Task's
// labels onto it.
type TaskRecord struct {
	metav1.ObjectMeta `json:"metadata"`
	Spec              TaskRecordSpec `json:"spec"`
}

// TaskRecordSpec is the record's content.
type TaskRecordSpec struct {
	TaskRef TaskReference `json:"taskRef"`
	// Model is the effective model, resolved through the WorkerPool when
	// the Task used one.
	Model          string       `json:"model,omitempty"`
	Phase          Phase        `json:"phase"`
	StartTime      *metav1.Time `json:"startTime,omitempty"`
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	Usage          *TaskUsage   `json:"usage,omitempty"`
}

// TaskReference names the Task a record was made from.
type TaskReference struct {
	Name string    `json:"name"`
	UID  types.UID `json:"uid"`
}
