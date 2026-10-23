-- Initial schema. Every record keeps its model JSON in body, the source of
-- truth read back into a story; the other columns are its key, the joins
-- and the rules Save applies (see the package documentation). Times in body
-- keep their nanoseconds; timestamptz columns are rounded to microseconds
-- and only order, compare and prune.

-- What a story is about: a Vikunja task, or a wiki page.
CREATE TABLE roots (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- model.Ref string: task:<host>/tasks/<id> or page:<host>/pages/<id>/<locale>.
    ref           text NOT NULL UNIQUE,
    kind          text NOT NULL CHECK (kind IN ('task', 'page')),
    instance      text NOT NULL,
    source_id     bigint NOT NULL,
    locale        text,
    -- Shown and searched; never part of the key.
    title         text,
    body          jsonb NOT NULL,
    -- Latest time of any record saved for the root; retention counts from it.
    last_activity timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CHECK ((kind = 'page') = (locale IS NOT NULL))
);
CREATE INDEX roots_last_activity ON roots (last_activity DESC NULLS LAST);

-- Events, keyed by source + instance + id.
CREATE TABLE events (
    id               bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source           text NOT NULL,
    instance         text NOT NULL,
    event_id         text NOT NULL,
    kind             text NOT NULL,
    -- model.Event.Time: the source time, else the (first) observed time.
    at               timestamptz NOT NULL,
    observed_at      timestamptz NOT NULL,
    -- Equal to observed_at unless later reads saw the same state again.
    last_observed_at timestamptz NOT NULL,
    -- A state seen by polling: the task it was seen on, and the series of
    -- one unchanged value (subject, kind and details). NULL otherwise.
    observed_subject text,
    series           text,
    -- The run the event names (model.Ref string), rewritten on re-keying.
    run_ref          text,
    -- Wiki page edits: the live record this archived version supersedes,
    -- and, for a live record, its page (model.Ref string).
    supersedes       text,
    live_page        text,
    body             jsonb NOT NULL,
    UNIQUE (source, instance, event_id),
    CHECK ((observed_subject IS NULL) = (series IS NULL)),
    CHECK (last_observed_at >= observed_at)
);
CREATE INDEX events_observed ON events (observed_subject, last_observed_at) WHERE observed_subject IS NOT NULL;
CREATE INDEX events_series ON events (series, last_observed_at) WHERE series IS NOT NULL;
CREATE INDEX events_run_ref ON events (run_ref) WHERE run_ref IS NOT NULL;
CREATE INDEX events_supersedes ON events (source, instance, supersedes) WHERE supersedes IS NOT NULL;
CREATE INDEX events_live_page ON events (live_page) WHERE live_page IS NOT NULL;

-- Runs, keyed by cluster + namespace + Task UID, or by pod for a run known
-- only from its start announcement until its UID is read.
CREATE TABLE runs (
    id        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    cluster   text NOT NULL,
    namespace text NOT NULL,
    uid       text,
    key_pod   text,
    -- model.Ref string of the key.
    ref       text NOT NULL UNIQUE,
    -- The run's pod, whatever the key: how a placeholder finds its UID.
    pod       text,
    body      jsonb NOT NULL,
    CHECK ((uid IS NULL) <> (key_pod IS NULL)),
    CHECK (key_pod IS NULL OR pod IS NOT DISTINCT FROM key_pod)
);
CREATE UNIQUE INDEX runs_uid ON runs (cluster, namespace, uid) WHERE uid IS NOT NULL;
CREATE UNIQUE INDEX runs_key_pod ON runs (cluster, namespace, key_pod) WHERE key_pod IS NOT NULL;
CREATE INDEX runs_pod ON runs (cluster, namespace, pod) WHERE pod IS NOT NULL;

-- Pull requests, keyed by instance + repository + number.
CREATE TABLE pull_requests (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    instance   text NOT NULL,
    repository text NOT NULL,
    number     integer NOT NULL,
    ref        text NOT NULL UNIQUE,
    branch     text NOT NULL,
    UNIQUE (instance, repository, number)
);

-- Check runs per commit. A later read never removes an earlier commit's.
CREATE TABLE check_runs (
    pull_request_id bigint NOT NULL REFERENCES pull_requests (id) ON DELETE CASCADE,
    check_id        bigint NOT NULL,
    sha             text NOT NULL,
    name            text NOT NULL,
    conclusion      text NOT NULL,
    body            jsonb NOT NULL,
    PRIMARY KEY (pull_request_id, check_id)
);
CREATE INDEX check_runs_sha ON check_runs (pull_request_id, sha);

-- Correlations, keyed by the pair of records they link, either way round.
CREATE TABLE correlations (
    id       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    pair_key text NOT NULL UNIQUE,
    from_ref text NOT NULL,
    to_ref   text NOT NULL,
    body     jsonb NOT NULL
);
CREATE INDEX correlations_from ON correlations (from_ref);
CREATE INDEX correlations_to ON correlations (to_ref);

-- Which records each root's story holds. A record can be in several stories.
CREATE TABLE root_events (
    root_id  bigint NOT NULL REFERENCES roots (id) ON DELETE CASCADE,
    event_id bigint NOT NULL REFERENCES events (id) ON DELETE CASCADE,
    PRIMARY KEY (root_id, event_id)
);
CREATE INDEX root_events_event ON root_events (event_id);

CREATE TABLE root_runs (
    root_id bigint NOT NULL REFERENCES roots (id) ON DELETE CASCADE,
    run_id  bigint NOT NULL REFERENCES runs (id) ON DELETE CASCADE,
    PRIMARY KEY (root_id, run_id)
);
CREATE INDEX root_runs_run ON root_runs (run_id);

CREATE TABLE root_pull_requests (
    root_id         bigint NOT NULL REFERENCES roots (id) ON DELETE CASCADE,
    pull_request_id bigint NOT NULL REFERENCES pull_requests (id) ON DELETE CASCADE,
    PRIMARY KEY (root_id, pull_request_id)
);
CREATE INDEX root_pull_requests_pr ON root_pull_requests (pull_request_id);

CREATE TABLE root_correlations (
    root_id        bigint NOT NULL REFERENCES roots (id) ON DELETE CASCADE,
    correlation_id bigint NOT NULL REFERENCES correlations (id) ON DELETE CASCADE,
    PRIMARY KEY (root_id, correlation_id)
);
CREATE INDEX root_correlations_correlation ON root_correlations (correlation_id);

-- Coverage of each source instance for each root, merged across reads.
CREATE TABLE coverage (
    root_id              bigint NOT NULL REFERENCES roots (id) ON DELETE CASCADE,
    source               text NOT NULL,
    instance             text NOT NULL,
    last_successful_read timestamptz,
    body                 jsonb NOT NULL,
    PRIMARY KEY (root_id, source, instance)
);

-- Where each read of a source left off: a Kelos resourceVersion, a GitHub
-- ETag or last read, the last read of one root (root_id set).
CREATE TABLE checkpoints (
    source     text NOT NULL,
    instance   text NOT NULL,
    scope      text NOT NULL,
    value      text NOT NULL,
    root_id    bigint REFERENCES roots (id) ON DELETE CASCADE,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (source, instance, scope)
);

-- Webhook deliveries received, one row per attempt (a redelivery reuses
-- the delivery id). Written by the webhook receiver, which comes later.
CREATE TABLE delivery_attempts (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source      text NOT NULL,
    instance    text NOT NULL,
    delivery_id text NOT NULL,
    event_type  text NOT NULL,
    received_at timestamptz NOT NULL,
    outcome     text NOT NULL CHECK (outcome IN ('accepted', 'ignored', 'rejected', 'failed')),
    detail      text,
    UNIQUE (source, instance, delivery_id, received_at)
);
CREATE INDEX delivery_attempts_received ON delivery_attempts (received_at);
