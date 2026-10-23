# Kelos objects

Fabricated Kelos 0.60.0 (`kelos.dev/v1alpha2`) objects in the namespace
`agents` of the cluster `cluster-a`, read by `internal/source/kelos`'s tests
through client-go's fake dynamic client. One directory per story:

- `tasks.json`: the namespace's `TaskList`;
- `taskrecords.json`: its `TaskRecordList`. Kelos names a record after its
  Task's UID and copies the Task's labels onto it;
- `records.golden.json`: what `Normalize` makes of them: runs and their
  correlations with Vikunja tasks. Generated; never edit it by hand:
  `go test ./internal/source/kelos -run TestNormalizeGolden -update`,
  then review the diff.

The spawners' labels use the domain `vikunja.example.com`. Each Task
carries a fabricated prompt and annotation to show that neither is read.

| Story | What it covers |
| --- | --- |
| `task-with-record` | A completed implementation run: the Task supplies creation, pod, job, message and labels; its record the interval, outcome, effective model (over the Task's requested one) and usage, whose cost `1234m` becomes `1.234`. |
| `record-only` | A failed task follow-up whose Task was cleaned up: interval, outcome, model, usage and labels come from the record; creation, pod, job and message are unknown. |
| `task-only` | A discussion run (name prefix `vikunja-discuss-`) that reported no usage, so has no record; its model comes from the deprecated `spec.model`. |
| `tasks-unfinished` | A running PR follow-up (open end) and a waiting automation Task (`waiting` outcome, no start yet, open end, model set by a WorkerPool and so unknown, no task label and so no correlation). |
| `same-name-retry` | A failed attempt known from its record and a retry with the same Task name and a new UID, known from its Task and record: two runs. |
