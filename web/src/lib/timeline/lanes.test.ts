import { describe, expect, test } from 'vitest';
import { eventKey, runKeyString } from '../keys';
import { event, PR, prEvent, run, story } from '../testing/builders';
import { golden, goldens } from '../testing/goldens';
import { buildRows, SECTIONS, type Row } from './lanes';

const ids = (rows: Row[]) => rows.map((r) => r.id);
const inSection = (rows: Row[], s: Row['section']) => rows.filter((r) => r.section === s);

describe('golden stories', () => {
	test('draw every event exactly once', () => {
		for (const [name, s] of goldens) {
			const drawn = buildRows(s).flatMap((r) => r.events.map(eventKey));
			expect(drawn.sort(), name).toEqual(s.events.map(eventKey).sort());
		}
	});

	test('keep lanes in section order, the root first', () => {
		for (const [name, s] of goldens) {
			const root = s.root.task ? 'task' : 'pages';
			const order = buildRows(s).map((r) =>
				r.section === root ? -1 : SECTIONS.indexOf(r.section)
			);
			expect(order, name).toEqual([...order].sort((a, b) => a - b));
		}
	});

	test('discuss-no-pr has no pull request lane', () => {
		expect(inSection(buildRows(golden('discuss-no-pr')), 'pulls')).toEqual([]);
	});

	test('failed-run-retry has one row per attempt, in order', () => {
		const runs = inSection(buildRows(golden('failed-run-retry')), 'runs').filter((r) =>
			r.id.startsWith('run:')
		);
		expect(runs.map((r) => r.subtitle)).toEqual([
			'implementation · attempt 1 · failed',
			'implementation · attempt 2 · succeeded'
		]);
	});

	test('announced-run-only has a run row with its announcement and no known bound', () => {
		const s = golden('announced-run-only');
		const row = buildRows(s).find((r) => r.id === `run:${runKeyString(s.runs[0]!.key)}`)!;
		expect(row.note).toBe('start and end unknown: no bar');
		expect(row.events.map((e) => e.kind)).toContain('run.announced');
		expect(row.spans.every((sp) => sp.start === undefined && sp.end === undefined)).toBe(true);
	});

	test('wiki-rooted-request roots on its page and keeps the live edit there', () => {
		const rows = buildRows(golden('wiki-rooted-request'));
		expect(rows[0]!.id).toBe('page:wiki.example.com/pages/57/en');
		expect(rows[0]!.title).toBe('widgets/design/storage-layout');
		expect(rows[0]!.events.some((e) => e.details?.version === 'live')).toBe(true);
	});

	test('implementation-merged has a check row per commit under its pull request', () => {
		const pulls = inSection(buildRows(golden('implementation-merged')), 'pulls');
		expect(pulls.map((r) => r.title)).toEqual([
			'#7 example-org/widgets',
			'checks aaaaaaa',
			'checks bbbbbbb'
		]);
	});
});

describe('event placement', () => {
	test('a person comment goes to their row, by source instance', () => {
		const e = event();
		const rows = buildRows(story({ events: [e] }));
		expect(ids(rows)).toContain('account:person:vikunja/vikunja.example.com:owner');
	});

	test('an agent comment joins its run only on an unambiguous identifier link', () => {
		const r = run();
		const linked = event({ actor: { login: 'agent', kind: 'agent' } });
		const guessed = event({ actor: { login: 'agent', kind: 'agent' } });
		const rows = buildRows(
			story({
				runs: [r],
				events: [linked, guessed],
				correlations: [
					{
						from: { kind: 'event', id: eventKey(linked) },
						to: { kind: 'run', id: runKeyString(r.key) },
						confidence: 'identifier',
						evidence: []
					},
					{
						from: { kind: 'event', id: eventKey(guessed) },
						to: { kind: 'run', id: runKeyString(r.key) },
						confidence: 'heuristic',
						evidence: []
					}
				]
			})
		);
		const runRow = rows.find((x) => x.id.startsWith('run:'))!;
		expect(runRow.events).toEqual([linked]);
		const account = rows.find((x) => x.id.startsWith('account:agent:'))!;
		expect(account.events).toEqual([guessed]);
		expect(account.subtitle).toContain('run not established');
	});

	test('a person request linked to a run stays on the person row', () => {
		const r = run();
		const request = event({ kind: 'request.created' });
		const rows = buildRows(
			story({
				runs: [r],
				events: [request],
				correlations: [
					{
						from: { kind: 'run', id: runKeyString(r.key) },
						to: { kind: 'event', id: eventKey(request) },
						confidence: 'identifier',
						evidence: []
					}
				]
			})
		);
		expect(rows.find((x) => x.id.startsWith('account:person:'))!.events).toEqual([request]);
	});

	test('pull request events make a pull request row even without its record', () => {
		const rows = buildRows(story({ events: [prEvent('pr.merged', '2026-03-01T10:00:00Z')] }));
		expect(ids(rows)).toContain('pr:github.example.com/example-org/widgets/pull/3');
	});

	test('a check with neither end known is noted on its row', () => {
		const rows = buildRows(
			story({
				pullRequests: [
					{
						key: PR,
						branch: 'b',
						checks: [
							{
								id: 1,
								sha: 'c'.repeat(40),
								name: 'lint',
								conclusion: 'neutral',
								interval: { start: { state: 'unknown' }, end: { state: 'unknown' } }
							}
						]
					}
				]
			})
		);
		expect(rows.find((r) => r.id.startsWith('checks:'))!.note).toBe(
			'check lint: start and end unknown, no bar'
		);
	});

	test('a run with no start, open with no last read, is noted on its row', () => {
		const r = run({
			outcome: 'running',
			interval: { start: { state: 'unknown' }, end: { state: 'open' } }
		});
		const rows = buildRows(story({ runs: [r] }));
		expect(rows.find((x) => x.id.startsWith('run:'))!.note).toBe(
			'start unknown, not ended at the last read: no bar'
		);
	});

	test('runs of one name are attempts only within one cluster and namespace', () => {
		const here = { cluster: 'cluster-a', namespace: 'agents' };
		const rows = buildRows(
			story({
				runs: [
					run({ key: { ...here, uid: 'uid-first' } }),
					run({ key: { ...here, uid: 'uid-retry' } }),
					run({ key: { cluster: 'cluster-a', namespace: 'other', uid: 'uid-other-namespace' } }),
					run({ key: { cluster: 'cluster-b', namespace: 'agents', uid: 'uid-other-cluster' } })
				]
			})
		);
		const subtitle = (uid: string) => rows.find((x) => x.id.endsWith(uid))!.subtitle;
		expect(subtitle('uid-first')).toBe('implementation · cluster-a/agents · attempt 1 · succeeded');
		expect(subtitle('uid-retry')).toBe('implementation · cluster-a/agents · attempt 2 · succeeded');
		expect(subtitle('uid-other-namespace')).toBe('implementation · cluster-a/other · succeeded');
		expect(subtitle('uid-other-cluster')).toBe('implementation · cluster-b/agents · succeeded');
	});

	test('an automation run goes to the automation lane', () => {
		const rows = buildRows(story({ runs: [run({ role: 'automation' })] }));
		expect(inSection(rows, 'automation')).toHaveLength(1);
		expect(inSection(rows, 'runs')).toEqual([]);
	});

	test('a pull request row carries its review waits', () => {
		const rows = buildRows(
			story({
				pullRequests: [{ key: PR, branch: 'b', checks: [] }],
				events: [
					prEvent('pr.review_requested', '2026-03-01T10:00:00Z', { details: { reviewer: 'alice' } })
				]
			})
		);
		expect(rows.find((r) => r.id.startsWith('pr:'))!.spans.map((s) => s.phase)).toEqual([
			'review-wait'
		]);
	});
});
