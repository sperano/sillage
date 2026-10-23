// The story JSON the Go service serves: internal/model's Story, field for
// field. Every time is an RFC 3339 string in UTC.

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

/** One Kelos Task attempt: exactly one of uid and pod is set. */
export interface RunKey {
	cluster: string;
	namespace: string;
	uid?: string;
	pod?: string;
}

export interface PRKey {
	instance: string;
	repository: string;
	number: number;
}

/** Exactly one of task and page is set. */
export interface Root {
	task?: TaskKey;
	page?: PageKey;
}

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
	details?: Record<string, string>;
	evidence: Evidence;
}

export type BoundState = 'known' | 'unknown' | 'open';

/** One end of an interval: at is set exactly when state is known. */
export interface Bound {
	state: BoundState;
	at?: string;
}

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

export interface Ref {
	kind: RefKind;
	id: string;
}

export interface Correlation {
	from: Ref;
	to: Ref;
	confidence: 'identifier' | 'heuristic';
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

// The key strings below are the Go keys' String forms, which correlations'
// refs carry.

export function eventKey(e: Event): string {
	return `${e.source}/${e.instance}/${e.id}`;
}

export function taskKeyString(k: TaskKey): string {
	return `${k.instance}/tasks/${k.id}`;
}

export function pageKeyString(k: PageKey): string {
	return `${k.instance}/pages/${k.id}/${k.locale}`;
}

export function runKeyString(k: RunKey): string {
	return k.uid
		? `${k.cluster}/${k.namespace}/uid/${k.uid}`
		: `${k.cluster}/${k.namespace}/pod/${k.pod ?? ''}`;
}

export function prKeyString(k: PRKey): string {
	return `${k.instance}/${k.repository}/pull/${k.number}`;
}

/** The coverage key of a source instance: source and instance. */
export function coverageKey(source: Source, instance: string): string {
	return `${source}/${instance}`;
}

/** An event's place on a timeline: its source time, else when it was observed. */
export function eventTime(e: Event): number {
	return Date.parse(e.sourceTime ?? e.observedAt);
}

/** A bound's time in milliseconds, or undefined unless it is known. */
export function boundTime(b: Bound): number | undefined {
	return b.state === 'known' && b.at !== undefined ? Date.parse(b.at) : undefined;
}
