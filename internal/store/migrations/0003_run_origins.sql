-- What started each Kelos Task: its correlation annotations (the Kelos
-- source's Origin, as opaque JSON), kept from the first read that saw the
-- Task. A TaskRecord copies a Task's labels but not its annotations, so
-- once the Task's TTL deleted it, only this row still says which page,
-- pull request or comment started the run. Kept apart from any story: the
-- Task can be gone before a story holding its run is saved.
CREATE TABLE run_origins (
    cluster       text NOT NULL,
    namespace     text NOT NULL,
    uid           text NOT NULL,
    origin        jsonb NOT NULL CHECK (jsonb_typeof(origin) = 'object'),
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (cluster, namespace, uid)
);
CREATE INDEX run_origins_first_seen ON run_origins (first_seen_at);
