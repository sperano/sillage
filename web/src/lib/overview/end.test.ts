import { describe, expect, it } from 'vitest';
import type { Run } from '../model';
import { afternoon, CLOSED_UNANSWERED, overviewGolden, rowOfTask } from '../testing/overview';
import type { ReviewWait } from './model';
import { endChips, reviewStateAt } from './end';

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

	it('shows a review still requested at the end when its pull request closed only after it', () => {
		expect(chips(202)).toEqual(['review: review requested: owner-gh', 'pr-open: PR #22 open']);
		expect(endChips(rowOfTask(page, 202), end)[0]?.note).toBe(
			'its pull request closed later with no answer recorded'
		);
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

	it('says a review whose pull request closed with no answer is unknown, not answered', () => {
		const row = rowOfTask(page, 201);
		const wait: ReviewWait = {
			...row.reviewWaits[0]!,
			interval: { ...row.reviewWaits[0]!.interval, end: { state: 'unknown' } },
			closed: CLOSED_UNANSWERED
		};
		expect(endChips({ ...row, reviewWaits: [wait] }, end)[0]).toEqual({
			kind: 'unknown',
			text: 'review requested: owner-gh, answer unknown',
			note: 'its pull request closed with no answer recorded'
		});
	});

	it('does not assert a task open when its history could hold the completion', () => {
		const row = { ...rowOfTask(page, 204), missingHistory: true };
		expect(endChips(row, end).map((c) => `${c.kind}: ${c.text}`)).toEqual([
			'pr-closed: PR #23 closed, not merged',
			'unknown: task completion unknown',
			'missing: history incomplete'
		]);
	});
});

describe('reviewStateAt', () => {
	const wait = rowOfTask(page, 201).reviewWaits[0]!;
	const open: ReviewWait = { ...wait, interval: { ...wait.interval, end: { state: 'open' } } };
	const closed: ReviewWait = {
		...wait,
		interval: { ...wait.interval, end: { state: 'unknown' } },
		closed: CLOSED_UNANSWERED
	};
	const answered: ReviewWait = {
		...wait,
		interval: { ...wait.interval, end: { state: 'known', at: '2026-03-10T14:40:00Z' } }
	};

	it('is requested from the request until the answer, or the last read', () => {
		expect(reviewStateAt(answered, afternoon('14:38'))).toBe('requested');
		expect(reviewStateAt(answered, afternoon('14:40'))).toBeUndefined();
		expect(reviewStateAt(open, end)).toBe('requested');
	});

	it('is requested until its pull request closed unanswered, unknown from then, nothing before', () => {
		expect(reviewStateAt(closed, afternoon('14:34'))).toBeUndefined();
		expect(reviewStateAt(closed, afternoon('14:49'))).toBe('requested');
		expect(reviewStateAt(closed, afternoon('14:50'))).toBe('unknown');
		expect(reviewStateAt(closed, end)).toBe('unknown');
	});
});
