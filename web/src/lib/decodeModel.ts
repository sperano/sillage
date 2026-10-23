// Runtime checks of the model JSON in model.ts. They mirror the Go model's
// Validate methods, so that a response the Go side would reject, or a field
// the types do not know, fails here rather than rendering wrong.

import {
	type Decoder,
	anyString,
	array,
	bool,
	decode,
	fail,
	nonNegativeInteger,
	object,
	oneOf,
	positiveInteger,
	record,
	refine,
	text,
	timestamp
} from './check';
import {
	ACTOR_KINDS,
	BOUND_STATES,
	CHECK_CONCLUSIONS,
	CONFIDENCES,
	EVENT_KINDS,
	EVENT_KIND_SOURCES,
	ID_KINDS,
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

export const runKey: Decoder<RunKey> = refine(
	object({ cluster: text, namespace: text }, { uid: text, pod: text }),
	(k, path) => {
		if ((k.uid === undefined) === (k.pod === undefined)) {
			fail(path, 'exactly one of uid and pod is required');
		}
	}
);

export const prKey: Decoder<PRKey> = object({
	instance: text,
	repository: text,
	number: positiveInteger
});

export const root: Decoder<Root> = refine(
	object({}, { task: taskKey, page: pageKey }),
	(r, path) => {
		if ((r.task === undefined) === (r.page === undefined)) {
			fail(path, 'exactly one of task and page is required');
		}
	}
);

const ref: Decoder<Ref> = object({ kind: oneOf(REF_KINDS), id: text });

const evidence: Decoder<Evidence> = object({ source, instance: text, ref: text }, { note: text });

const account: Decoder<Account> = refine(
	object({ kind: actorKind }, { login: text }),
	(a, path) => {
		if ((a.login === undefined) !== (a.kind === 'unknown')) {
			fail(path, 'a login and a kind, or kind unknown without a login, are required');
		}
	}
);

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
		{ sourceTime: timestamp, executor, details: record(anyString) }
	),
	(e, path) => {
		const sources: readonly string[] = EVENT_KIND_SOURCES[e.kind];
		if (!sources.includes(e.source)) fail(path, `kind ${e.kind} cannot come from ${e.source}`);
		if (e.idKind === 'fingerprint' && !FINGERPRINT.test(e.id)) {
			fail(`${path}.id`, `${JSON.stringify(e.id)} is not a fingerprint`);
		}
	}
);

const boundFields = object({ state: oneOf(BOUND_STATES) }, { at: timestamp });

function bound(canBeOpen: boolean): Decoder<Bound> {
	return (value, path) => {
		const b = boundFields(value, path);
		if (b.state === 'known') {
			if (b.at === undefined) fail(path, 'a known bound needs a time');
			return { state: 'known', at: b.at };
		}
		if (b.at !== undefined) fail(path, `a ${b.state} bound has no time`);
		if (b.state === 'open' && !canBeOpen) fail(path, 'only an end can be open');
		return { state: b.state };
	};
}

export const interval: Decoder<Interval> = refine(
	object({ start: bound(false), end: bound(true) }),
	(i, path) => {
		if (i.start.state === 'known' && i.end.state === 'known') {
			if (Date.parse(i.end.at) < Date.parse(i.start.at)) fail(path, 'ends before it starts');
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
		if (Date.parse(g.end) <= Date.parse(g.start)) fail(path, 'a gap needs an end after its start');
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

export const story: Decoder<Story> = object({
	root,
	events: unique(array(event), (e) => `${e.source}/${e.instance}/${e.id}`),
	runs: unique(array(run), (r) => JSON.stringify(r.key)),
	pullRequests: unique(array(pullRequest), (p) => JSON.stringify(p.key)),
	correlations: unique(array(correlation), (c) =>
		[refString(c.from), refString(c.to)].sort().join(' <-> ')
	),
	coverage: coverageList
});

/** Checks value against the Story JSON, throwing a DecodeError on a mismatch. */
export function parseStory(value: unknown): Story {
	return decode(story, value);
}

function refString(r: Ref): string {
	return `${r.kind}:${r.id}`;
}

function nonEmpty<T>(decoder: Decoder<T[]>): Decoder<T[]> {
	return refine(decoder, (list, path) => {
		if (list.length === 0) fail(path, 'at least one is required');
	});
}

// unique rejects two elements with the same key: the Go model never builds
// a story holding one record twice.
function unique<T>(decoder: Decoder<T[]>, key: (item: T) => string): Decoder<T[]> {
	return refine(decoder, (list, path) => {
		const seen = new Set<string>();
		list.forEach((item, i) => {
			const k = key(item);
			if (seen.has(k)) fail(`${path}[${i}]`, `duplicate record ${k}`);
			seen.add(k);
		});
	});
}
