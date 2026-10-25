import { readdirSync, readFileSync, existsSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { describe, expect, test } from 'vitest';
import { DecodeError } from './check';
import { parseStory } from './decodeModel';
import { EVENT_KIND_SOURCES, OBSERVED_KINDS } from './model';
import { STORIES_DIR, goldenStories, goldenStory } from '../testing/goldenStories';

const REPO_ROOT = resolve(import.meta.dirname, '../../..');
const BASE_STORY = 'implementation-merged';
// The Go model's event contract (internal/model's TestEventContractGolden).
const EVENT_CONTRACT = 'testdata/fixtures/contract/event.golden.json';
// In BASE_STORY: a task created (a recorded action), then its bucket seen
// by polling, and its one run's key.
const RECORDED_EVENT = 0;
const OBSERVED_EVENT = 1;
const RUN_KEY = {
	cluster: 'cluster-a',
	namespace: 'agents',
	uid: '00000000-0000-4000-8000-000000000102'
};

type Path = readonly (string | number)[];

type Node = Record<string | number, unknown>;

// get walks path from value; every step must exist.
function get(value: unknown, path: Path): unknown {
	let node = value;
	for (const step of path) node = (node as Node)[step];
	return node;
}

function parentOf(value: unknown, path: Path): Node {
	return get(value, path.slice(0, -1)) as Node;
}

function set(value: unknown, path: Path, to: unknown): void {
	parentOf(value, path)[path.at(-1) as string | number] = to;
}

function remove(value: unknown, path: Path): void {
	delete parentOf(value, path)[path.at(-1) as string | number];
}

// base returns a fresh copy of the base story's JSON with change applied.
function base(change: (story: unknown) => void): unknown {
	const story = goldenStory(BASE_STORY);
	change(story);
	return story;
}

describe('golden stories', () => {
	test('every story directory has a golden file the tests load', () => {
		const dir = join(REPO_ROOT, STORIES_DIR);
		const names = readdirSync(dir).filter((name) =>
			existsSync(join(dir, name, 'story.golden.json'))
		);
		expect(names.length).toBeGreaterThan(0);
		expect([...goldenStories.keys()]).toEqual(names.sort());
	});

	test.each([...goldenStories.keys()])('%s decodes unchanged', (name) => {
		const json = goldenStory(name);
		expect(parseStory(json)).toStrictEqual(json);
	});
});

describe('optional fields the golden stories lack', () => {
	test.each<[string, (story: unknown) => void]>([
		[
			'a run with every optional field',
			(s) => {
				set(s, ['runs', 0, 'job'], 'job-a');
				set(s, ['runs', 0, 'project'], 'widgets');
				set(s, ['runs', 0, 'created'], '2026-03-03T18:05:30.123456789Z');
				set(s, ['runs', 0, 'message'], 'done');
				set(s, ['runs', 0, 'model'], 'model-a');
				set(s, ['runs', 0, 'usage'], { costUSD: '0.42', inputTokens: 0, outputTokens: 12 });
			}
		],
		[
			'a waiting run with an open end',
			(s) => {
				set(s, ['runs', 0, 'outcome'], 'waiting');
				set(s, ['runs', 0, 'interval', 'end'], { state: 'open' });
			}
		],
		[
			'a pending check with an open end',
			(s) => {
				set(s, ['pullRequests', 0, 'checks', 1, 'conclusion'], 'pending');
				set(s, ['pullRequests', 0, 'checks', 1, 'interval', 'end'], { state: 'open' });
			}
		],
		['coverage notes', (s) => set(s, ['coverage', 0, 'notes'], ['commit bbbb: checks not read'])],
		['a run keyed by pod', (s) => set(s, ['runs', 0, 'key'], runKeyedByPod(s))],
		[
			'a time with an offset',
			(s) => set(s, ['events', 0, 'observedAt'], '2026-03-03T14:05:00-04:00')
		],
		[
			'an interval a nanosecond long',
			(s) =>
				set(s, ['runs', 0, 'interval'], {
					start: { state: 'known', at: '2026-03-03T18:05:37.000000001Z' },
					end: { state: 'known', at: '2026-03-03T18:05:37.000000002Z' }
				})
		],
		[
			'a gap within one millisecond',
			(s) =>
				set(
					s,
					['coverage', 0, 'gaps'],
					[{ start: '2026-03-03T19:00:00.0001Z', end: '2026-03-03T19:00:00.0002Z', reason: 'down' }]
				)
		],
		[
			'a state seen again by a later read',
			(s) => set(s, ['events', OBSERVED_EVENT, 'lastObservedAt'], '2026-03-03T18:20:00Z')
		],
		[
			'a Kelos observation of the run',
			(s) => (get(s, ['events']) as unknown[]).push(runObserved())
		],
		[
			'two correlations whose ids hold what a joined pair would',
			(s) => {
				set(s, ['correlations', 0, 'from'], { kind: 'event', id: 'a' });
				set(s, ['correlations', 0, 'to'], { kind: 'event', id: 'b <-> event:c' });
				set(s, ['correlations', 1, 'from'], { kind: 'event', id: 'a <-> event:b' });
				set(s, ['correlations', 1, 'to'], { kind: 'event', id: 'c' });
			}
		]
	])('%s decodes unchanged', (_, change) => {
		const json = base(change);
		expect(parseStory(json)).toStrictEqual(json);
	});
});

// runObserved is a watch's observation of BASE_STORY's run, as the Kelos
// source writes it.
function runObserved(): unknown {
	return {
		source: 'kelos',
		instance: 'cluster-a',
		id: `tasks/${RUN_KEY.uid}@4101/modified`,
		idKind: 'source',
		kind: 'run.observed',
		observedAt: '2026-03-03T18:06:30Z',
		actor: { kind: 'unknown' },
		related: { run: RUN_KEY },
		details: { object: 'task', change: 'modified', phase: 'Running', resourceVersion: '4101' },
		evidence: { source: 'kelos', instance: 'cluster-a', ref: 'tasks/agents/widgets-102' }
	};
}

function runKeyedByPod(story: unknown): unknown {
	return { cluster: 'cluster-a', namespace: 'agents', pod: get(story, ['runs', 0, 'pod']) };
}

describe('a story the Go model would reject', () => {
	test.each<[string, (story: unknown) => void, string]>([
		['an unknown field', (s) => set(s, ['extra'], 1), '$: unexpected field extra'],
		['a missing list', (s) => remove(s, ['coverage']), '$: missing field coverage'],
		[
			'a null optional field',
			(s) => set(s, ['events', 0, 'sourceTime'], null),
			'expected a string'
		],
		[
			'an unknown event kind',
			(s) => set(s, ['events', 0, 'kind'], 'task.renamed'),
			'unknown value'
		],
		[
			'a kind from the wrong source',
			(s) => set(s, ['events', 0, 'source'], 'github'),
			'kind task.created cannot come from github'
		],
		[
			'a last observed time on a recorded action',
			(s) => set(s, ['events', RECORDED_EVENT, 'lastObservedAt'], '2026-03-03T18:20:00Z'),
			'kind task.created is no state seen by polling'
		],
		[
			'a last observed time before the first',
			(s) => set(s, ['events', OBSERVED_EVENT, 'lastObservedAt'], '2026-03-03T18:04:59Z'),
			'is before observedAt'
		],
		[
			'a fingerprint id that is not one',
			(s) => set(s, ['events', 1, 'id'], 'sha256:ABC'),
			'is not a fingerprint'
		],
		[
			'an unknown actor with a login',
			(s) => set(s, ['events', 1, 'actor', 'login'], 'someone'),
			'kind unknown without a login'
		],
		[
			'a known bound without a time',
			(s) => remove(s, ['runs', 0, 'interval', 'start', 'at']),
			'a known bound needs a time'
		],
		[
			'an unknown bound with a time',
			(s) => set(s, ['runs', 0, 'interval', 'start', 'state'], 'unknown'),
			'a unknown bound has no time'
		],
		[
			'an open start',
			(s) => set(s, ['runs', 0, 'interval', 'start'], { state: 'open' }),
			'only an end can be open'
		],
		[
			'an interval ending before it starts',
			(s) => set(s, ['runs', 0, 'interval', 'end', 'at'], '2026-03-03T18:00:00Z'),
			'ends before it starts'
		],
		[
			'an interval ending a nanosecond before it starts',
			(s) =>
				set(s, ['runs', 0, 'interval'], {
					start: { state: 'known', at: '2026-03-03T18:05:37.000000002Z' },
					end: { state: 'known', at: '2026-03-03T18:05:37.000000001Z' }
				}),
			'ends before it starts'
		],
		[
			'a finished run with an open end',
			(s) => set(s, ['runs', 0, 'interval', 'end'], { state: 'open' }),
			'only a waiting or running run, has an open end'
		],
		[
			'a running run with a known end',
			(s) => set(s, ['runs', 0, 'outcome'], 'running'),
			'only a waiting or running run, has an open end'
		],
		[
			'a run keyed by another pod',
			(s) => set(s, ['runs', 0, 'key'], { cluster: 'c', namespace: 'n', pod: 'other' }),
			'must name the same pod'
		],
		[
			'a run key with both a uid and a pod',
			(s) => set(s, ['runs', 0, 'key', 'pod'], 'p'),
			'exactly one of uid and pod'
		],
		[
			'a run keyed by pod beside the run keyed by UID that names that pod',
			(s) => set(s, ['runs', 1], { ...(get(s, ['runs', 0]) as object), key: runKeyedByPod(s) }),
			'a run keyed by UID names the same pod'
		],
		['a run without evidence', (s) => set(s, ['runs', 0, 'evidence'], []), 'at least one'],
		[
			'a cost that is not a plain decimal',
			(s) => set(s, ['runs', 0, 'usage'], { costUSD: '1e3' }),
			'is not a non-negative decimal'
		],
		[
			'a negative token count',
			(s) => set(s, ['runs', 0, 'usage'], { inputTokens: -1 }),
			'non-negative integer'
		],
		[
			'a pending check with a known end',
			(s) => set(s, ['pullRequests', 0, 'checks', 0, 'conclusion'], 'pending'),
			'only a pending check, has an open end'
		],
		[
			'a check id seen twice',
			(s) => set(s, ['pullRequests', 0, 'checks', 1, 'id'], 9001),
			'duplicate record 9001'
		],
		[
			'a correlation to itself',
			(s) =>
				set(s, ['correlations', 0, 'to'], {
					kind: 'event',
					id: 'vikunja/vikunja.example.com/comments/5101'
				}),
			'cannot correlate with itself'
		],
		[
			'a correlation recorded from each end',
			(s) =>
				set(s, ['correlations', 1], {
					...(get(s, ['correlations', 3]) as object),
					from: get(s, ['correlations', 3, 'to']),
					to: get(s, ['correlations', 3, 'from'])
				}),
			'duplicate record'
		],
		[
			'an event seen twice',
			(s) => set(s, ['events', 1], get(s, ['events', 0])),
			'duplicate record vikunja/vikunja.example.com/tasks/102'
		],
		[
			'a root with a task and a page',
			(s) => set(s, ['root', 'page'], { instance: 'wiki.example.com', id: 1, locale: 'en' }),
			'exactly one of task and page'
		],
		[
			'an id beyond what JavaScript represents',
			(s) => set(s, ['root', 'task', 'id'], 2 ** 53),
			'expected a safe integer'
		],
		[
			'a time that is not RFC 3339',
			(s) => set(s, ['events', 0, 'observedAt'], '2026-03-03 18:05:00'),
			'expected an RFC 3339 time'
		],
		[
			'a gap ending before it starts',
			(s) =>
				set(
					s,
					['coverage', 0, 'gaps'],
					[{ start: '2026-03-03T19:00:00Z', end: '2026-03-03T18:00:00Z', reason: 'down' }]
				),
			'a gap needs an end after its start'
		],
		[
			'a gap ending at its start, written another way',
			(s) =>
				set(
					s,
					['coverage', 0, 'gaps'],
					[{ start: '2026-03-03T19:00:00.5Z', end: '2026-03-03T19:00:00.500Z', reason: 'down' }]
				),
			'a gap needs an end after its start'
		],
		['an empty note', (s) => set(s, ['coverage', 0, 'notes'], ['']), 'expected a non-empty string']
	])('%s fails', (_, change, problem) => {
		expect(() => parseStory(base(change))).toThrow(DecodeError);
		expect(() => parseStory(base(change))).toThrow(problem);
	});

	test('the error names where it failed', () => {
		const json = base((s) => set(s, ['runs', 0, 'role'], 'chore'));
		expect(() => parseStory(json)).toThrow(
			expect.objectContaining({ path: '$.runs[0].role' }) as DecodeError
		);
	});
});

// The phase-markers story's first run holds, in order: a complete blocker
// wait, an interrupted agent span, a complete wiki login and two
// inconsistent pairs; its retry the interrupted agent span of its Job's
// first pod, the second pod's agent span in progress and a login whose
// start was not recorded.
const PHASE_STORY = 'phase-markers';
const FAILED_RUN = 0;
const RETRY = 1;
const RETRY_DEAD_POD = 0;
const RETRY_LIVE_POD = 1;
const RETRY_LOGIN = 2;

function phased(change: (story: unknown) => void): unknown {
	const story = goldenStory(PHASE_STORY);
	change(story);
	return story;
}

function phasePath(run: number, phase: number, ...rest: Path): Path {
	return ['runs', run, 'phases', phase, ...rest];
}

describe('a phase the Go model would not make', () => {
	test.each<[string, (story: unknown) => void, string]>([
		[
			'an unknown kind',
			(s) => set(s, phasePath(FAILED_RUN, 2, 'start', 'kind'), 'wiki-request'),
			'unknown value'
		],
		[
			'another kind’s emitter',
			(s) => set(s, phasePath(FAILED_RUN, 2, 'start', 'emitter'), 'entrypoint'),
			'is emitted by wiki_gql'
		],
		[
			'a start with an outcome',
			(s) => set(s, phasePath(FAILED_RUN, 2, 'start', 'outcome'), 'ok'),
			'a start has no outcome'
		],
		[
			'an end with another kind’s outcome',
			(s) => set(s, phasePath(FAILED_RUN, 2, 'end', 'outcome'), 'unblocked'),
			'one of its outcomes'
		],
		[
			'a login end without attempts',
			(s) => remove(s, phasePath(FAILED_RUN, 2, 'end', 'attempts')),
			'counts its attempts'
		],
		[
			'a phase with no edge',
			(s) => {
				remove(s, phasePath(RETRY, RETRY_LOGIN, 'end'));
			},
			'a phase has a recorded edge'
		],
		[
			'an interrupted phase closed at a time no edge recorded',
			(s) =>
				set(s, phasePath(FAILED_RUN, 1, 'interval', 'end'), {
					state: 'known',
					at: '2026-03-14T10:40:00Z'
				}),
			'do not make a phase interrupted'
		],
		[
			'an interval on an inconsistent pair',
			(s) =>
				set(s, phasePath(FAILED_RUN, 3, 'interval'), {
					start: { state: 'unknown' },
					end: { state: 'unknown' }
				}),
			'only an inconsistent phase, has no interval'
		],
		[
			'an inconsistent pair called complete',
			(s) => set(s, phasePath(FAILED_RUN, 4, 'state'), 'complete'),
			'do not make a phase complete'
		],
		[
			'a bound off its edge',
			(s) => set(s, phasePath(FAILED_RUN, 2, 'interval', 'start', 'at'), '2026-03-14T10:06:01Z'),
			'a known bound is a recorded edge'
		],
		[
			'a phase in progress on an ended run',
			(s) => {
				set(s, ['runs', RETRY, 'outcome'], 'failed');
				set(s, ['runs', RETRY, 'interval', 'end'], { state: 'known', at: '2026-03-14T11:20:00Z' });
			},
			'a in-progress phase of a failed run'
		],
		[
			'phases in progress from two pods of one run',
			(s) => {
				set(s, phasePath(RETRY, RETRY_DEAD_POD, 'state'), 'in-progress');
				set(s, phasePath(RETRY, RETRY_DEAD_POD, 'interval', 'end'), { state: 'open' });
			},
			'phases in progress from pods'
		],
		['an empty list', (s) => set(s, ['runs', RETRY, 'phases'], []), 'at least one is required'],
		[
			'a span twice',
			(s) => {
				const phases = get(s, ['runs', RETRY, 'phases']) as unknown[];
				phases.push(structuredClone(phases[0]));
			},
			'duplicate'
		],
		['a short span id', (s) => set(s, phasePath(RETRY, 0, 'span'), 'abc'), 'not a span id']
	])('%s fails', (_, change, problem) => {
		expect(() => parseStory(phased(change))).toThrow(DecodeError);
		expect(() => parseStory(phased(change))).toThrow(problem);
	});
});

test('a retried Job’s earlier pod has its phase interrupted while the run runs', () => {
	const phases = parseStory(goldenStory(PHASE_STORY)).runs[RETRY]?.phases ?? [];
	expect(phases.map((p) => p.state)).toEqual(['interrupted', 'in-progress', 'start-not-recorded']);
	expect(phases[RETRY_DEAD_POD]?.pod).not.toBe(phases[RETRY_LIVE_POD]?.pod);
});

interface EventContract {
	kinds: Record<string, string[]>;
	observedKinds: string[];
	fields: { name: string; optional: boolean }[];
}

function eventContract(): EventContract {
	return JSON.parse(readFileSync(join(REPO_ROOT, EVENT_CONTRACT), 'utf8')) as EventContract;
}

function sortedSources(
	kinds: Readonly<Record<string, readonly string[]>>
): Record<string, string[]> {
	return Object.fromEntries(Object.entries(kinds).map(([k, v]) => [k, [...v].sort()]));
}

describe('the Go event contract', () => {
	test('every event kind, from the sources the Go model allows', () => {
		expect(sortedSources(EVENT_KIND_SOURCES)).toEqual(sortedSources(eventContract().kinds));
	});

	test('the kinds of a state seen by polling', () => {
		expect([...OBSERVED_KINDS].sort()).toEqual(eventContract().observedKinds);
	});

	test('every field decodes, and only the optional ones may be absent', () => {
		const fields = eventContract().fields;
		const extra = { executor: { name: 'agent-opus', kind: 'agent' }, details: { k: 'v' } };
		const json = base((s) => {
			Object.assign(get(s, ['events', RECORDED_EVENT]) as Node, extra);
			Object.assign(get(s, ['events', OBSERVED_EVENT]) as Node, extra, {
				lastObservedAt: '2026-03-03T18:20:00Z'
			});
		});
		const recorded = get(json, ['events', RECORDED_EVENT]) as Node;
		const observed = get(json, ['events', OBSERVED_EVENT]) as Node;
		const names = new Set([...Object.keys(recorded), ...Object.keys(observed)]);
		expect([...names].sort()).toEqual(fields.map((f) => f.name).sort());
		expect(parseStory(json)).toStrictEqual(json);
		for (const { name, optional } of fields) {
			if (optional) continue;
			const missing = base((s) => remove(s, ['events', RECORDED_EVENT, name]));
			expect(() => parseStory(missing), name).toThrow(`missing field ${name}`);
		}
	});
});
