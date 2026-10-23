// The JSON of internal/model's Story, as the API serves it and the golden
// stories under testdata/fixtures/stories hold it. Times are RFC 3339
// strings in UTC. An optional field is absent, never null.
//
// The string unions are the values this build knows. The API can send a
// value added to the Go model after this build, so code that names a value
// in words falls back to the value itself rather than trusting the union.

export type Source = 'vikunja' | 'kelos' | 'github' | 'wiki';

export type EventKind =
	| 'task.created'
	| 'task.bucket_observed'
	| 'task.label_observed'
	| 'task.assignee_observed'
	| 'task.done'
	| 'page.created'
	| 'page.edited'
	| 'page.moved'
	| 'comment.created'
	| 'request.created'
	| 'run.announced'
	| 'pr.opened'
	| 'pr.review_requested'
	| 'pr.review_request_removed'
	| 'pr.review_submitted'
	| 'pr.merged'
	| 'pr.closed'
	| 'pr.reopened';

export type ActorKind = 'person' | 'agent' | 'service' | 'unknown';

export interface TaskKey {
	instance: string;
	id: number;
}

export interface PageKey {
	instance: string;
	id: number;
	locale: string;
}

/** A Task UID, or, when no Kelos object of the run was read, its pod. */
export type RunKey = { cluster: string; namespace: string } & (
	{ uid: string; pod?: undefined } | { pod: string; uid?: undefined }
);

export interface PRKey {
	instance: string;
	repository: string;
	number: number;
}

export type Root = { task: TaskKey; page?: undefined } | { page: PageKey; task?: undefined };

/** A polling observation has kind unknown and no login. */
export interface Account {
	login?: string;
	kind: ActorKind;
}

export interface Executor {
	name: string;
	kind: ActorKind;
}

export interface Related {
	task?: TaskKey;
	page?: PageKey;
	run?: RunKey;
	pr?: PRKey;
}

export interface Evidence {
	source: Source;
	instance: string;
	ref: string;
	note?: string;
}

export interface Event {
	source: Source;
	instance: string;
	id: string;
	idKind: 'source' | 'fingerprint';
	kind: EventKind;
	/** Absent for a state seen by polling. */
	sourceTime?: string;
	observedAt: string;
	actor: Account;
	executor?: Executor;
	related: Related;
	details?: Readonly<Record<string, string>>;
	evidence: Evidence;
}

/** Only an end can be open. */
export type Bound = { state: 'known'; at: string } | { state: 'unknown' | 'open'; at?: undefined };

export interface Interval {
	start: Bound;
	end: Bound;
}

export type RunRole =
	| 'implementation'
	| 'discussion'
	| 'task-follow-up'
	| 'pr-follow-up'
	| 'wiki-follow-up'
	| 'conflict-fix'
	| 'automation'
	| 'unknown';

export type Outcome = 'succeeded' | 'failed' | 'waiting' | 'running' | 'unknown';

export interface Usage {
	/** A decimal string, kept exact. */
	costUSD?: string;
	inputTokens?: number;
	outputTokens?: number;
}

export interface Run {
	key: RunKey;
	taskName?: string;
	pod?: string;
	job?: string;
	agent?: string;
	project?: string;
	role: RunRole;
	created?: string;
	interval: Interval;
	outcome: Outcome;
	message?: string;
	model?: string;
	usage?: Usage;
	evidence: Evidence[];
}

export type CheckConclusion =
	| 'pending'
	| 'success'
	| 'failure'
	| 'cancelled'
	| 'timed_out'
	| 'neutral'
	| 'skipped'
	| 'action_required'
	| 'stale';

export interface CheckRun {
	id: number;
	sha: string;
	name: string;
	interval: Interval;
	conclusion: CheckConclusion;
}

export interface PullRequest {
	key: PRKey;
	branch: string;
	checks: CheckRun[];
}

export type RefKind = 'task' | 'page' | 'run' | 'pr' | 'event';

/** A record by its key's string form; see keys.ts. */
export interface Ref {
	kind: RefKind;
	id: string;
}

export type Confidence = 'identifier' | 'heuristic';

export interface Correlation {
	from: Ref;
	to: Ref;
	confidence: Confidence;
	/** One of several candidates the evidence fits; none was chosen. */
	ambiguous?: boolean;
	evidence: Evidence[];
}

export interface Gap {
	start: string;
	end: string;
	reason: string;
}

export interface Coverage {
	source: Source;
	instance: string;
	/** Absent if the source was never read without error. */
	lastSuccessfulRead?: string;
	gaps?: Gap[];
	notes?: string[];
}

export interface Story {
	root: Root;
	events: Event[];
	runs: Run[];
	pullRequests: PullRequest[];
	correlations: Correlation[];
	coverage: Coverage[];
}
