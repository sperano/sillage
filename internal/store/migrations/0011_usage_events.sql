-- Usage events: one model response (or failed request, or reported cost) a
-- runner in a Kelos run's pod exported as OpenTelemetry, read back from the
-- telemetry store (Loki). Immutable and kept apart from any story: a usage
-- event is keyed by its pod UID, event name, stream (session or
-- conversation) and sequence, so that one delivered twice is one row. body
-- is its model.UsageEvent; the other columns are what the reads filter by.
-- Joined to a run only when costs are computed, by pod (run_pods), else by
-- Task name.
CREATE TABLE usage_events (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    cluster     text NOT NULL,
    namespace   text NOT NULL,
    pod         text NOT NULL,
    pod_uid     text NOT NULL,
    task_name   text,
    runner      text NOT NULL,
    kind        text NOT NULL,
    event       text NOT NULL,
    stream      text NOT NULL,
    seq         text NOT NULL,
    at          timestamptz NOT NULL,
    received_at timestamptz NOT NULL DEFAULT now(),
    body        jsonb NOT NULL,
    UNIQUE (pod_uid, event, stream, seq)
);
CREATE INDEX usage_events_pod ON usage_events (cluster, namespace, pod);
CREATE INDEX usage_events_task ON usage_events (cluster, namespace, task_name);
CREATE INDEX usage_events_at ON usage_events (at);

-- How far the usage feed has read: every event before read_through was
-- read. One row per feed instance; it never moves backwards.
CREATE TABLE usage_feed (
    instance     text PRIMARY KEY,
    read_through timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL
);
