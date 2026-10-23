import { readdirSync, existsSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { describe, expect, test } from 'vitest';
import { DecodeError } from './check';
import { parseStory } from './decodeModel';
import { STORIES_DIR, goldenStories, goldenStory } from '../testing/goldenStories';

const REPO_ROOT = resolve(import.meta.dirname, '../../..');
const BASE_STORY = 'implementation-merged';

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
