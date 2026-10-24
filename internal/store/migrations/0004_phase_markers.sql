-- Phase markers: one edge of one span a wrapper in a Kelos run's pod
-- reported (POST /ingest/marker), kept apart from any story. A marker is
-- keyed by the pod UID its token was verified for, its span and its edge,
-- so that a re-sent marker is one row. body is its model.Marker without
-- the join; run_uid, confidence and join_evidence are the join (by the
-- pod, 'identifier', else by the Task name and time, 'heuristic'), all
-- NULL while it is unattached. The join is in the marker's own cluster and
-- namespace: a pod speaks for a Task of its namespace only.
CREATE TABLE markers (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    cluster       text NOT NULL,
    namespace     text NOT NULL,
    pod           text NOT NULL,
    pod_uid       text NOT NULL,
    span          text NOT NULL,
    edge          text NOT NULL CHECK (edge IN ('start', 'end')),
    -- The Task name the body gave, a hint for the join by name.
    task_name     text,
    at            timestamptz NOT NULL,
    received_at   timestamptz NOT NULL,
    body          jsonb NOT NULL,
    run_uid       text,
    confidence    text CHECK (confidence IN ('identifier', 'heuristic')),
    join_evidence jsonb,
    UNIQUE (pod_uid, span, edge),
    CHECK ((run_uid IS NULL) = (confidence IS NULL) AND (run_uid IS NULL) = (join_evidence IS NULL))
);
CREATE INDEX markers_run ON markers (cluster, namespace, run_uid) WHERE run_uid IS NOT NULL;
-- What a save joins again: every marker not joined by its pod yet.
CREATE INDEX markers_unsettled_pod ON markers (cluster, namespace, pod)
    WHERE confidence IS DISTINCT FROM 'identifier';
CREATE INDEX markers_unsettled_task ON markers (cluster, namespace, task_name)
    WHERE confidence IS DISTINCT FROM 'identifier';
CREATE INDEX markers_received ON markers (received_at);

-- The pods whose markers were refused for passing the limit per pod: the
-- coverage of the stories of their runs says so.
CREATE TABLE marker_limits (
    pod_uid          text PRIMARY KEY,
    cluster          text NOT NULL,
    namespace        text NOT NULL,
    pod              text NOT NULL,
    first_refused_at timestamptz NOT NULL,
    last_refused_at  timestamptz NOT NULL,
    refused          bigint NOT NULL CHECK (refused > 0)
);
CREATE INDEX marker_limits_last_refused ON marker_limits (last_refused_at);

-- Every pod each Kelos run was seen with, by Task UID: a Task's
-- status.podName names only its latest pod, and a retried Job has a
-- second. ref is the Kelos object that named the pod. Kept apart from any
-- story, like run_origins: what joins a marker to its run by pod.
CREATE TABLE run_pods (
    cluster       text NOT NULL,
    namespace     text NOT NULL,
    uid           text NOT NULL,
    pod           text NOT NULL,
    ref           text NOT NULL,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (cluster, namespace, uid, pod)
);
CREATE INDEX run_pods_pod ON run_pods (cluster, namespace, pod);
CREATE INDEX run_pods_first_seen ON run_pods (first_seen_at);

-- The join by name looks runs up by Task name.
CREATE INDEX runs_task_name ON runs (cluster, namespace, (body ->> 'taskName')) WHERE uid IS NOT NULL;
