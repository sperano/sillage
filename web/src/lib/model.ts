// The JSON of internal/model's Story, as the read API serves it unchanged.
// Each type mirrors its Go struct's JSON field for field, as a union where
// the Go model allows only some combinations of its fields; decodeModel.ts checks a
// response against them, so that a model change fails the web tests
// (decodeModel.test.ts loads every golden story) instead of rendering wrong.
//
// Every time is an RFC 3339 string, in UTC in a story: kept as sent, never
// stored as a Date; eventTime and boundTime read one as milliseconds.
// Optional fields are those Go omits when empty.

/** The kinds of system a record comes from. */
export const SOURCES = ['vikunja', 'kelos', 'github', 'wiki'] as const;
export type Source = (typeof SOURCES)[number];

/** Identifies a Vikunja task: the instance's host and the task's id. */
export interface TaskKey {
	instance: string;
	id: number;
}

/** Identifies a Wiki.js page: host, page id and locale, never its path. */
export interface PageKey {
	instance: string;
	id: number;
	locale: string;
}

/**
 * Identifies one run, one Kelos Task attempt: by its Task UID, or, when
 * only a start announcement was read, by its pod.
 */
export type RunKey = { cluster: string; namespace: string } & (
	{ uid: string; pod?: never } | { pod: string; uid?: never }
);

/** Identifies a GitHub pull request; repository is owner/name. */
export interface PRKey {
	instance: string;
	repository: string;
	number: number;
}

/** What a story is about: a task, or a page when the work has no task. */
export type Root = { task: TaskKey; page?: never } | { page: PageKey; task?: never };

/** The kinds of record a Ref points to. */
export const REF_KINDS = ['task', 'page', 'run', 'pr', 'event'] as const;
export type RefKind = (typeof REF_KINDS)[number];

/** Points to a record by its Go key's String form. */
export interface Ref {
	kind: RefKind;
	id: string;
}

/** What happened, and the sources each kind can come from. */
export const EVENT_KIND_SOURCES = {
	'task.created': ['vikunja'],
	'task.bucket_observed': ['vikunja'],
	'task.label_observed': ['vikunja'],
	'task.assignee_observed': ['vikunja'],
	'task.done': ['vikunja'],
	'page.created': ['wiki'],
	'page.edited': ['wiki'],
	'page.moved': ['wiki'],
	'comment.created': ['vikunja', 'wiki', 'github'],
	'request.created': ['vikunja', 'wiki', 'github'],
	'run.announced': ['vikunja', 'wiki', 'github'],
	'pr.opened': ['github'],
	'pr.review_requested': ['github'],
	'pr.review_request_removed': ['github'],
	'pr.review_submitted': ['github'],
	'pr.merged': ['github'],
	'pr.closed': ['github'],
	'pr.reopened': ['github']
} as const satisfies Record<string, readonly Source[]>;
export type EventKind = keyof typeof EVENT_KIND_SOURCES;
// Object.keys types its result string[]; these keys are EventKind by
// definition. Frozen, as the decoder checks against it.
export const EVENT_KINDS = Object.freeze(Object.keys(EVENT_KIND_SOURCES) as EventKind[]);

/** Where an event id comes from: the source, or a fingerprint when it has none. */
export const ID_KINDS = ['source', 'fingerprint'] as const;
export type IDKind = (typeof ID_KINDS)[number];

/** What kind of party an account or executor is. */
export const ACTOR_KINDS = ['person', 'agent', 'service', 'unknown'] as const;
export type ActorKind = (typeof ACTOR_KINDS)[number];

/**
 * The account a source says acted. A state seen by polling has no known
 * actor: kind unknown, and no login.
 */
export type Account =
	{ kind: Exclude<ActorKind, 'unknown'>; login: string } | { kind: 'unknown'; login?: never };

/** Who actually acted, when known and different from the account. */
export interface Executor {
	name: string;
	kind: ActorKind;
}

/** The keys the source record itself names. */
export interface Related {
	task?: TaskKey;
	page?: PageKey;
	run?: RunKey;
	pr?: PRKey;
}

/** Points to the source record behind a fact. */
export interface Evidence {
	source: Source;
	instance: string;
	ref: string;
	note?: string;
}

/** One thing that happened, as one source recorded it. */
export interface Event {
	source: Source;
	instance: string;
	id: string;
	idKind: IDKind;
	kind: EventKind;
	/** When the source says it happened; absent for a state seen by polling. */
	sourceTime?: string;
	/** When the collector read it. */
	observedAt: string;
	actor: Account;
	executor?: Executor;
	related: Related;
	/** Safe, short attributes: never content, prompts or secrets. */
	details?: Record<string, string>;
	evidence: Evidence;
}

/**
 * What is known about one end of an interval: a source recorded the time,
 * no source did, or (an end only) it had not ended at the last read.
 */
export const BOUND_STATES = ['known', 'unknown', 'open'] as const;
export type BoundState = (typeof BOUND_STATES)[number];

interface KnownBound {
	state: 'known';
	at: string;
}

/** One end of an interval; at is set exactly when state is known. */
export type Bound = KnownBound | { state: 'unknown' | 'open'; at?: never };

/** An interval's start: known or unknown, never open. */
export type StartBound = KnownBound | { state: 'unknown'; at?: never };

/** A span whose ends are each known, unknown or (the end only) open. */
export interface Interval {
	start: StartBound;
	end: Bound;
}

/** Why a run was started. */
export const RUN_ROLES = [
	'implementation',
	'discussion',
	'task-follow-up',
	'pr-follow-up',
	'wiki-follow-up',
	'conflict-fix',
	'automation',
	'unknown'
] as const;
export type RunRole = (typeof RUN_ROLES)[number];

/** A run's current state or how it ended, as far as the evidence says. */
export const OUTCOMES = ['succeeded', 'failed', 'waiting', 'running', 'unknown'] as const;
export type Outcome = (typeof OUTCOMES)[number];

/** The outcomes of a run that has not ended: its interval's end is open. */
export const UNFINISHED_OUTCOMES: readonly Outcome[] = ['waiting', 'running'];

/** The cost and tokens a run reported; each is absent when not reported. */
export interface Usage {
	/** US dollars as an exact decimal string ("0.42"), never a float. */
	costUSD?: string;
	inputTokens?: number;
	outputTokens?: number;
}

/**
 * One Kelos Task attempt. Its times are the Kubernetes API server's and the
 * Kelos controller's lifecycle times, not a measure of the agent's work.
 */
export interface Run {
	key: RunKey;
	/** A display label and a correlation hint, never an identity: a retry reuses it. */
	taskName?: string;
	pod?: string;
	job?: string;
	agent?: string;
	project?: string;
	role: RunRole;
	/** When the Task object was created, when known. */
	created?: string;
	interval: Interval;
	outcome: Outcome;
	message?: string;
	model?: string;
	usage?: Usage;
	evidence: Evidence[];
}

/** A check run's result, as GitHub names it, or pending. */
export const CHECK_CONCLUSIONS = [
	'pending',
	'success',
	'failure',
	'cancelled',
	'timed_out',
	'neutral',
	'skipped',
	'action_required',
	'stale'
] as const;
export type CheckConclusion = (typeof CHECK_CONCLUSIONS)[number];

/** One check on one commit; each SHA keeps its own results. */
export interface CheckRun {
	id: number;
	sha: string;
	name: string;
	interval: Interval;
	conclusion: CheckConclusion;
}

/** A pull request and its checks per commit; its other history is events. */
export interface PullRequest {
	key: PRKey;
	branch: string;
	checks: CheckRun[];
}

/** How strongly the evidence establishes a correlation. */
export const CONFIDENCES = ['identifier', 'heuristic'] as const;
export type Confidence = (typeof CONFIDENCES)[number];

/**
 * A link between two records the sources do not link themselves. A
 * heuristic one is uncertain, never a causal link; an ambiguous one is one
 * of several candidates, none chosen.
 */
export interface Correlation {
	from: Ref;
	to: Ref;
	confidence: Confidence;
	ambiguous?: boolean;
	evidence: Evidence[];
}

/**
 * A span for which a source's records are known to be missing. What
 * happened inside it is unknown: never idle time, never work.
 */
export interface Gap {
	start: string;
	end: string;
	reason: string;
}

/** How far one source instance has been read. */
export interface Coverage {
	source: Source;
	instance: string;
	/** Absent if the source was never read without error; anything after it is unknown. */
	lastSuccessfulRead?: string;
	gaps?: Gap[];
	/** What the reads left unknown that no time range bounds. */
	notes?: string[];
}

/** Everything known about one root, every list in a deterministic order. */
export interface Story {
	root: Root;
	events: Event[];
	runs: Run[];
	pullRequests: PullRequest[];
	correlations: Correlation[];
	coverage: Coverage[];
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
	return b.state === 'known' ? Date.parse(b.at) : undefined;
}
