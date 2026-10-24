// Runtime checks of the model JSON in model.ts. They mirror the Go model's
// Validate methods, so that a response the Go side would reject, or a field
// the types do not know, fails here rather than rendering wrong.

import {
	type Decoder,
	anyString,
	array,
	bool,
	compareTimes,
	decode,
	fail,
	nonNegativeInteger,
	object,
	oneOf,
	positiveInteger,
	nonEmpty,
	record,
	refine,
	text,
	timestamp,
	unique
} from './check';
import {
	ACTOR_KINDS,
	BOUND_STATES,
	CHECK_CONCLUSIONS,
	CONFIDENCES,
	EVENT_KINDS,
	EVENT_KIND_SOURCES,
	ID_KINDS,
	OBSERVED_KINDS,
	OUTCOMES,
	REF_KINDS,
	RUN_ROLES,
	SOURCES,
	UNFINISHED_OUTCOMES,
	type Account,
	type Bound,
	type CheckRun,
	type Correlation,
	type Coverage,
	type Event,
	type Evidence,
	type Executor,
	type Gap,
	type Interval,
	type PRKey,
	type PageKey,
	type PullRequest,
	type Ref,
	type Related,
	type Root,
	type Run,
	type RunKey,
	type StartBound,
	type Story,
	type TaskKey,
	type Usage
} from './model';

const source = oneOf(SOURCES);
const actorKind = oneOf(ACTOR_KINDS);

export const taskKey: Decoder<TaskKey> = object({ instance: text, id: positiveInteger });

export const pageKey: Decoder<PageKey> = object({
	instance: text,
	id: positiveInteger,
	locale: text
});

const runKeyFields = object({ cluster: text, namespace: text }, { uid: text, pod: text });

export const runKey: Decoder<RunKey> = (value, path) => {
	const { cluster, namespace, uid, pod } = runKeyFields(value, path);
	if (uid !== undefined && pod === undefined) return { cluster, namespace, uid };
	if (pod !== undefined && uid === undefined) return { cluster, namespace, pod };
	return fail(path, 'exactly one of uid and pod is required');
};

export const prKey: Decoder<PRKey> = object({
	instance: text,
	repository: text,
	number: positiveInteger
});

const rootFields = object({}, { task: taskKey, page: pageKey });

export const root: Decoder<Root> = (value, path) => {
	const { task, page } = rootFields(value, path);
	if (task !== undefined && page === undefined) return { task };
	if (page !== undefined && task === undefined) return { page };
	return fail(path, 'exactly one of task and page is required');
};

const ref: Decoder<Ref> = object({ kind: oneOf(REF_KINDS), id: text });

const evidence: Decoder<Evidence> = object({ source, instance: text, ref: text }, { note: text });

const accountFields = object({ kind: actorKind }, { login: text });

const account: Decoder<Account> = (value, path) => {
	const { kind, login } = accountFields(value, path);
	if (kind === 'unknown' && login === undefined) return { kind };
	if (kind !== 'unknown' && login !== undefined) return { kind, login };
	return fail(path, 'a login and a kind, or kind unknown without a login, are required');
};

const executor: Decoder<Executor> = object({ name: text, kind: actorKind });

const related: Decoder<Related> = object(
	{},
	{ task: taskKey, page: pageKey, run: runKey, pr: prKey }
);

// A fingerprint id: "sha256:" and a lowercase hex SHA-256 digest.
const FINGERPRINT = /^sha256:[0-9a-f]{64}$/;

export const event: Decoder<Event> = refine(
	object(
		{
			source,
			instance: text,
			id: text,
			idKind: oneOf(ID_KINDS),
			kind: oneOf(EVENT_KINDS),
			observedAt: timestamp,
			actor: account,
			related,
			evidence
		},
		{ sourceTime: timestamp, lastObservedAt: timestamp, executor, details: record(anyString) }
	),
	(e, path) => {
		const sources: readonly string[] = EVENT_KIND_SOURCES[e.kind];
		if (!sources.includes(e.source)) fail(path, `kind ${e.kind} cannot come from ${e.source}`);
		if (e.idKind === 'fingerprint' && !FINGERPRINT.test(e.id)) {
			fail(`${path}.id`, `${JSON.stringify(e.id)} is not a fingerprint`);
		}
		checkLastObserved(e, path);
	}
);

const observedKinds: readonly string[] = OBSERVED_KINDS;

// checkLastObserved mirrors the Go model's validateObserved: only a state
// seen by polling has a last observed time, no earlier than its first.
function checkLastObserved(e: Event, path: string): void {
	if (e.lastObservedAt === undefined) return;
	const at = `${path}.lastObservedAt`;
	if (!observedKinds.includes(e.kind)) fail(at, `kind ${e.kind} is no state seen by polling`);
	if (compareTimes(e.lastObservedAt, e.observedAt) < 0) fail(at, 'is before observedAt');
}

const boundFields = object({ state: oneOf(BOUND_STATES) }, { at: timestamp });

const endBound: Decoder<Bound> = (value, path) => {
	const b = boundFields(value, path);
	if (b.state === 'known') {
		if (b.at === undefined) fail(path, 'a known bound needs a time');
		return { state: 'known', at: b.at };
	}
	if (b.at !== undefined) fail(path, `a ${b.state} bound has no time`);
	return { state: b.state };
};

const startBound: Decoder<StartBound> = (value, path) => {
	const b = endBound(value, path);
	if (b.state === 'known') return b;
	if (b.state === 'open') fail(path, 'only an end can be open');
	return { state: 'unknown' };
};

export const interval: Decoder<Interval> = refine(
	object({ start: startBound, end: endBound }),
	(i, path) => {
		if (i.start.state === 'known' && i.end.state === 'known') {
			if (compareTimes(i.end.at, i.start.at) < 0) fail(path, 'ends before it starts');
		}
	}
);

// A non-negative decimal without sign, exponent or redundant leading zero,
// as the Go model's decimalPattern.
const DECIMAL = /^(0|[1-9][0-9]*)(\.[0-9]+)?$/;

const usage: Decoder<Usage> = object(
	{},
	{
		costUSD: refine(anyString, (s, path) => {
			if (!DECIMAL.test(s)) fail(path, `${JSON.stringify(s)} is not a non-negative decimal`);
		}),
		inputTokens: nonNegativeInteger,
		outputTokens: nonNegativeInteger
	}
);

export const run: Decoder<Run> = refine(
	object(
		{
			key: runKey,
			role: oneOf(RUN_ROLES),
			interval,
			outcome: oneOf(OUTCOMES),
			evidence: nonEmpty(array(evidence))
		},
		{
			taskName: text,
			pod: text,
			job: text,
			agent: text,
			project: text,
			created: timestamp,
			message: text,
			model: text,
			usage
		}
	),
	(r, path) => {
		if (r.key.pod !== undefined && r.pod !== r.key.pod) {
			fail(path, 'a run keyed by pod must name the same pod');
		}
		const unfinished = UNFINISHED_OUTCOMES.includes(r.outcome);
		if (unfinished !== (r.interval.end.state === 'open')) {
			fail(path, 'a waiting or running run, and only a waiting or running run, has an open end');
		}
	}
);

const checkRun: Decoder<CheckRun> = refine(
	object({
		id: positiveInteger,
		sha: text,
		name: text,
		interval,
		conclusion: oneOf(CHECK_CONCLUSIONS)
	}),
	(c, path) => {
		if ((c.conclusion === 'pending') !== (c.interval.end.state === 'open')) {
			fail(path, 'a pending check, and only a pending check, has an open end');
		}
	}
);

export const pullRequest: Decoder<PullRequest> = object({
	key: prKey,
	branch: text,
	checks: unique(array(checkRun), (c) => String(c.id))
});

export const correlation: Decoder<Correlation> = refine(
	object(
		{ from: ref, to: ref, confidence: oneOf(CONFIDENCES), evidence: nonEmpty(array(evidence)) },
		{ ambiguous: bool }
	),
	(c, path) => {
		if (c.from.kind === c.to.kind && c.from.id === c.to.id) {
			fail(path, 'a record cannot correlate with itself');
		}
	}
);

const gap: Decoder<Gap> = refine(
	object({ start: timestamp, end: timestamp, reason: text }),
	(g, path) => {
		if (compareTimes(g.end, g.start) <= 0) fail(path, 'a gap needs an end after its start');
	}
);

export const coverage: Decoder<Coverage> = object(
	{ source, instance: text },
	{ lastSuccessfulRead: timestamp, gaps: array(gap), notes: array(text) }
);

/** Coverage of several source instances, at most one record each. */
export const coverageList: Decoder<Coverage[]> = unique(
	array(coverage),
	(c) => `${c.source}/${c.instance}`
);

// A run keyed by pod when a run keyed by UID names the same pod: one
// attempt, which the collector re-keys by UID rather than keeping twice.
export function checkRunPods(runs: Run[], path: string): void {
	const podsOfUIDRuns = new Set(
		runs.flatMap((r) =>
			r.key.uid !== undefined && r.pod !== undefined ? [podKey(r.key, r.pod)] : []
		)
	);
	runs.forEach((r, i) => {
		if (r.key.pod !== undefined && podsOfUIDRuns.has(podKey(r.key, r.key.pod))) {
			fail(`${path}[${i}]`, 'a run keyed by UID names the same pod');
		}
	});
}

function podKey(key: RunKey, pod: string): string {
	return JSON.stringify([key.cluster, key.namespace, pod]);
}

export const story: Decoder<Story> = object({
	root,
	events: unique(array(event), (e) => `${e.source}/${e.instance}/${e.id}`),
	runs: refine(
		unique(array(run), (r) => JSON.stringify(r.key)),
		checkRunPods
	),
	pullRequests: unique(array(pullRequest), (p) => JSON.stringify(p.key)),
	// The pair of ends, in either direction: Go's Correlation.Pair.
	correlations: unique(array(correlation), (c) =>
		JSON.stringify([refString(c.from), refString(c.to)].sort())
	),
	coverage: coverageList
});

/** Checks value against the Story JSON, throwing a DecodeError on a mismatch. */
export function parseStory(value: unknown): Story {
	return decode(story, value);
}

// A ref as one string: its kind, from a fixed list without a colon, then
// its id.
function refString(r: Ref): string {
	return `${r.kind}:${r.id}`;
}
