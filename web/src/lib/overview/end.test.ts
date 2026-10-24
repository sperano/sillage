import { describe, expect, it } from 'vitest';
import type { Run } from '../model';
import { afternoon, overviewGolden, rowOfTask } from '../testing/overview';
import { endChips } from './end';

const page = overviewGolden();
const end = afternoon('15:00');

function chips(id: number): string[] {
	return endChips(rowOfTask(page, id), end).map((c) => `${c.kind}: ${c.text}`);
}

describe('endChips', () => {
	it('keeps a task open when its pull request closed without a merge', () => {
		expect(chips(204)).toEqual([
			'pr-closed: PR #23 closed, not merged',
			'not-done: task not marked done'
		]);
	});

	it('shows a merge and a completion apart', () => {
		expect(chips(206)).toEqual(['pr-merged: PR #24 merged', 'done: task marked done']);
	});

	it('keeps a failure beside the retry still running', () => {
		expect(chips(203)).toEqual(['running: running', 'failed: a run failed']);
	});

	it('shows a review still requested at the last read', () => {
		expect(chips(201)).toEqual(['review: review requested: owner-gh', 'pr-open: PR #21 open']);
	});

	it('says when a run and the history are unknown, and nothing more', () => {
		expect(chips(205)).toEqual(['unknown: run state unknown', 'missing: history incomplete']);
	});

	it('says nothing is running once the run ended at the window end', () => {
		const row = rowOfTask(page, 203);
		const retry = row.runs[1]!;
		const ended: Run = {
			...retry,
			interval: { ...retry.interval, end: { state: 'known', at: '2026-03-10T15:00:00Z' } },
			outcome: 'succeeded'
		};
		const kinds = endChips({ ...row, runs: [row.runs[0]!, ended] }, end).map((c) => c.kind);
		expect(kinds).toEqual(['failed']);
	});

	it('says a queued run with no recorded creation or start is unknown, not waiting', () => {
		const row = rowOfTask(page, 203);
		const queued: Run = {
			...row.runs[1]!,
			created: undefined,
			interval: { start: { state: 'unknown' }, end: { state: 'open' } },
			outcome: 'waiting'
		};
		const kinds = endChips({ ...row, runs: [queued] }, end).map((c) => c.kind);
		expect(kinds).toEqual(['unknown']);
		const since = { ...queued, created: '2026-03-10T14:50:00Z' };
		expect(endChips({ ...row, runs: [since] }, end).map((c) => c.kind)).toEqual(['waiting']);
	});

	it('notes that an open item is only known until the last read', () => {
		const running = endChips(rowOfTask(page, 203), end).find((c) => c.kind === 'running');
		expect(running?.note).toBe('not ended at the last read');
	});
});
