package vikunja

import "time"

// The types below decode Vikunja's /api/v1 JSON. They keep only the fields
// sillage reads; Vikunja sends more.

// User is a Vikunja account as embedded in tasks and comments.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

// Label is a label as embedded in a task.
type Label struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// Bucket is a Kanban bucket the task sits in, one per Kanban view. Vikunja
// returns them only when the task is read with expand=buckets, and older
// versions not at all.
type Bucket struct {
	ID            int64  `json:"id"`
	Title         string `json:"title"`
	ProjectViewID int64  `json:"project_view_id"`
}

// Task is a Vikunja task as GET /tasks/{id} returns it. Vikunja sends the
// zero time 0001-01-01T00:00:00Z for an unset DoneAt.
type Task struct {
	ID        int64     `json:"id"`
	ProjectID int64     `json:"project_id"`
	Title     string    `json:"title"`
	Created   time.Time `json:"created"`
	CreatedBy *User     `json:"created_by"`
	Done      bool      `json:"done"`
	DoneAt    time.Time `json:"done_at"`
	Labels    []Label   `json:"labels"`
	Assignees []User    `json:"assignees"`
	// BucketID is set by Vikunja versions before project views; later ones
	// leave it 0 and fill Buckets instead.
	BucketID int64    `json:"bucket_id"`
	Buckets  []Bucket `json:"buckets"`
}

// Comment is a task comment. Body is HTML.
type Comment struct {
	ID      int64     `json:"id"`
	Body    string    `json:"comment"`
	Author  User      `json:"author"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
}

// Snapshot is one read of a task and all its comments.
type Snapshot struct {
	// Instance is the base URL the task was read from.
	Instance string
	Task     Task
	// Comments are oldest first.
	Comments []Comment
	// ObservedAt is when the task itself was read: the time its current
	// state (done, buckets, labels, assignees) is known to have held.
	ObservedAt time.Time
}
