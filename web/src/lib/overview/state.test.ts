import { describe, expect, it } from 'vitest';
import type { Run } from '../model';
import { afternoon, overviewGolden, rowOfTask } from '../testing/overview';
import { instantOf, kelosUnknownAt, knownAt, runStateAt } from './state';

const KEY = { cluster: 'cluster-a', namespace: 'agents', uid: 'u' };

function run(fields: Partial<Run>): Run {
	return {
		key: KEY,
		role: 'implementation',
		interval: { start: { state: 'unknown' }, end: { state: 'unknown' } },
		outcome: 'unknown',
		evidence: [{ source: 'kelos', instance: 'cluster-a', ref: 'tasks/agents/t' }],
		...fields
	};
}

const known = (clock: string) => ({ state: 'known' as const, at: `2026-03-10T${clock}:00Z` });

describe('runStateAt', () => {
	const ended = run({
		interval: { start: known('14:10'), end: known('14:30') },
		outcome: 'succeeded'
	});
	const open = run({
		interval: { start: known('14:10'), end: { state: 'open' } },
		outcome: 'running'
	});
	const queued = run({
		created: '2026-03-10T14:05:00Z',
		interval: { start: { state: 'unknown' }, end: { state: 'open' } },
		outcome: 'waiting'
	});
	const created = run({ ...ended, created: '2026-03-10T14:05:00Z' });
	const unknownEnd = run({ interval: { start: known('14:10'), end: { state: 'unknown' } } });
	const read = afternoon('14:40');

	it.each([
		['before its start', ended, '14:05', 'not-running'],
		['between its bounds', ended, '14:20', 'running'],
		['at its end', ended, '14:30', 'not-running'],
		['created, not started', created, '14:07', 'waiting'],
		['before its creation', created, '14:00', 'not-running'],
		['open, before the last read', open, '14:35', 'running'],
		['open, after the last read', open, '14:45', 'unknown'],
		['queued since its creation', queued, '14:35', 'waiting'],
		['end unknown', unknownEnd, '14:20', 'unknown'],
		['nothing known', run({}), '14:20', 'unknown']
	] as const)('%s', (_name, r, clock, want) => {
		expect(runStateAt(r, afternoon(clock), read)).toBe(want);
	});
});

describe('the golden afternoon', () => {
	const page = overviewGolden();

	it('has A, C and D executing at 14:25, and E unknown', () => {
		const at = instantOf(page.rows, afternoon('14:25'));
		expect(at.executing.map((e) => e.row.title)).toEqual([
			'Health check',
			'Cache metrics',
			'Route rules'
		]);
		expect(at.unknown.map((r) => r.title)).toEqual(['Storage cleanup']);
	});

	it('has C executing once its retry runs, and E unknown whatever the time: its run has no bounds', () => {
		const at = instantOf(page.rows, afternoon('14:52'));
		expect(at.executing.map((e) => e.row.title)).toEqual(['Cache metrics']);
		expect(at.unknown.map((r) => r.title)).toEqual(['Storage cleanup']);
	});

	it('knows where the Kelos gap is', () => {
		const e = rowOfTask(page, 205);
		expect(kelosUnknownAt(e, afternoon('14:25'))).toBe(true);
		expect(kelosUnknownAt(e, afternoon('14:55'))).toBe(false);
		const kelos = e.coverage.find((c) => c.source === 'kelos')!;
		expect(knownAt(kelos, afternoon('14:50'))).toBe(true);
		expect(knownAt(kelos, afternoon('15:10'))).toBe(false);
	});
});
