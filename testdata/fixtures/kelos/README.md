# Kelos objects

Fabricated Kelos 0.60.0 (`kelos.dev/v1alpha2`) objects in the namespace
`agents` of the cluster `cluster-a`, read by `internal/source/kelos`'s tests
through client-go's fake dynamic client. One directory per story:

- `tasks.json`: the namespace's `TaskList`;
- `taskrecords.json`: its `TaskRecordList`. Kelos names a record after its
  Task's UID and copies the Task's labels onto it;
- `remembered.json`, optional: the Origins (`kelos.Origin`, by Task UID)
  a caller kept of Tasks this read no longer lists, given to `Normalize`
  as `Snapshot.Remembered`;
- `records.golden.json`: what `Normalize` makes of them: runs and their
  correlations with Vikunja tasks and pull requests.
  Generated; never edit it by hand:
  `go test ./internal/source/kelos -run TestNormalizeGolden -update`,
  then review the diff.

The spawners' labels and correlation annotations use the domain
`vikunja.example.com`. Each Task carries a fabricated prompt and an
annotation outside that domain to show that neither is read.

| Story | What it covers |
| --- | --- |
| `task-with-record` | A completed implementation run: the Task supplies creation, pod, job, message and labels; its record the interval, outcome, effective model (over the Task's requested one) and usage, whose cost `1234m` becomes `1.234`. |
| `record-only` | A failed task follow-up whose Task was cleaned up: interval, outcome, model, usage and labels come from the record; creation, pod, job and message are unknown. |
| `task-only` | A discussion run (name prefix `vikunja-discuss-`) that reported no usage, so has no record; its model comes from the deprecated `spec.model`. |
| `tasks-unfinished` | A running PR follow-up (open end) and a waiting automation Task (`waiting` outcome, no start yet, open end, model set by a WorkerPool and so unknown, no task label and so no correlation). |
| `same-name-retry` | A failed attempt known from its record and a retry with the same Task name and a new UID, known from its Task and record: two runs. |
| `annotated-no-labels` | Tasks with correlation annotations and no labels (role unknown): a PR follow-up and a merge Task joined to their pull request; kubectl's last-applied annotation is not read. |
| `record-only-remembered` | A PR follow-up read from its TaskRecord alone, whose Origin was kept when its Task was read: joined to its pull request by that Origin, the evidence saying when it was read and why it was kept. |
