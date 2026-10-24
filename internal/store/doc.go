// Package store persists normalized model records, source checkpoints and
// coverage in PostgreSQL, so that history survives source-side cleanup
// (Kelos Task TTLs, TaskRecord expiry) and sillage restarts.
//
// ConfigFromEnv reads a Config from the environment (EnvDatabaseURL, never
// logged, and EnvRetention); Open connects to the Config's database through
// pgx and applies the migrations embedded in the binary, recorded in
// schema_migrations. Save stores one read's records about one root (see
// its rules). Commit is the collection loop's write: in one transaction,
// one read stored by Save's rules, with the checkpoints it advanced, or
// checkpoints alone; the read's coverage of each source instance it
// carries replaces the root's stored coverage of that instance, and the
// root's coverage of the other instances is kept. Checkpoints lists the
// checkpoints back. The Reader methods (Story, SearchRoots, Coverage)
// serve the API. AcceptDelivery, RecordDelivery and DeliveryAccepted keep
// the webhook delivery attempts the ingest paths received, apart from
// every story's records. Prune applies the Config's retention policy (by
// default keep everything).
//
// Each record is stored as its model JSON, the source of truth read back
// into a story, next to the columns that key it (events by source,
// instance and id; runs by cluster, namespace and Task UID, or pod for a
// placeholder; pull requests by instance, repository and number, their
// check runs by id per commit; correlations by the pair they link) and
// that its rules need. Records are shared: a root's story is the records
// its saves added (root_events, root_runs, ...), so a run that two stories
// name is stored once. Coverage is per root and source instance.
//
// The migration runner is this package's own (migrate.go, under two
// hundred lines): embedded, numbered, forward-only SQL files, applied with
// their version rows in one transaction under an advisory lock, so that two
// replicas starting together migrate once and a failure leaves nothing
// half-applied. Each row keeps its file's name and SHA-256, so a database
// whose applied migration was since edited is refused. tern (v2.4) does
// the same and more, but its migrate package templates SQL with sprig,
// whose dependency tree the image would ship and license for no use; goose
// (v3.29) runs through database/sql rather than pgx, and its module
// requires every driver it supports. None of the three needs CGO.
package store
