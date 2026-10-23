// The state of a row at the window's end, as short chips: what was still
// running or waiting, the pull requests' states, the task's completion, and
// whether the history is incomplete. A closed, unmerged pull request never
// reads as the task being done: the task's state is Vikunja's.

import { boundTime, type Interval, type Run } from '../model';
import { prKeyString } from '../keys';
import type { OverviewRow, ReviewWait } from './model';
import { runLastRead, runStateAt } from './state';

export type ChipKind =
	| 'running'
	| 'waiting'
	| 'unknown'
	| 'failed'
	| 'review'
	| 'pr-open'
	| 'pr-merged'
	| 'pr-closed'
	| 'done'
	| 'not-done'
	| 'missing';

export interface Chip {
	kind: ChipKind;
	text: string;
	/** Says more than the text: where an open item was last seen, why a task reads open. */
	note?: string;
}

/**
 * Whether the interval had not ended by t: started before it, and ended
 * after it or not at the last read. An open end says only that it had not
 * ended when last read; the chip's note says so.
 */
function goingAt(i: Interval, t: number): boolean {
	const start = boundTime(i.start);
	if (start === undefined || start >= t) return false;
	const end = boundTime(i.end);
	return end !== undefined ? end >= t : i.end.state === 'open';
}

const OPEN_NOTE = 'not ended at the last read';

function reviewText(w: ReviewWait): string {
	const who = w.reviewer ?? (w.team === undefined ? undefined : `team ${w.team}`);
	return who === undefined ? 'review requested' : `review requested: ${who}`;
}

/** The chips of the row's state at the window's end, to (in milliseconds). */
export function endChips(row: OverviewRow, to: number): Chip[] {
	const chips: Chip[] = row.runs.flatMap((run) => runChips(row, run, to));
	const failed = row.runs.filter((r) => r.outcome === 'failed').length;
	if (failed > 0) {
		chips.push({
			kind: 'failed',
			text: failed === 1 ? 'a run failed' : `${failed} runs failed`,
			note: 'a failure stays here while a retry runs'
		});
	}
	for (const w of row.reviewWaits) {
		if (goingAt(w.interval, to)) {
			chips.push({
				kind: 'review',
				text: reviewText(w),
				note: w.interval.end.state === 'open' ? OPEN_NOTE : undefined
			});
		}
	}
	chips.push(...prChips(row));
	chips.push(...doneChips(row));
	if (row.missingHistory) {
		chips.push({
			kind: 'missing',
			text: 'history incomplete',
			note: 'part of the window is unknown: nothing is measured there'
		});
	}
	return chips;
}

/**
 * A run's chip at the window's end: running or waiting when its records
 * say it had not ended, unknown when they cannot say, none when it had.
 */
function runChips(row: OverviewRow, run: Run, to: number): Chip[] {
	const queued = run.outcome === 'waiting';
	if (goingAt(run.interval, to) || (queued && run.interval.end.state === 'open')) {
		return [
			{
				kind: queued ? 'waiting' : 'running',
				text: queued ? 'waiting to run' : 'running',
				note: run.interval.end.state === 'open' ? OPEN_NOTE : undefined
			}
		];
	}
	if (runStateAt(run, to, runLastRead(row, run)) === 'unknown') {
		return [
			{ kind: 'unknown', text: 'run state unknown', note: 'its records do not say whether it ran' }
		];
	}
	return [];
}

function prChips(row: OverviewRow): Chip[] {
	return row.pullRequests.flatMap((pr): Chip[] => {
		const name = `PR #${pr.key.number}`;
		switch (pr.state) {
			case 'open':
				return [{ kind: 'pr-open', text: `${name} open`, note: prKeyString(pr.key) }];
			case 'merged':
				return [{ kind: 'pr-merged', text: `${name} merged`, note: prKeyString(pr.key) }];
			case 'closed':
				return [
					{ kind: 'pr-closed', text: `${name} closed, not merged`, note: prKeyString(pr.key) }
				];
			case 'unknown':
				return [];
		}
	});
}

/**
 * The task's completion in the window, or, when a pull request was closed
 * without a merge and no completion was recorded, that the task was not
 * marked done: the close does not close it.
 */
function doneChips(row: OverviewRow): Chip[] {
	if (row.marks.some((m) => m.kind === 'task.done')) {
		return [{ kind: 'done', text: 'task marked done' }];
	}
	if (row.pullRequests.some((p) => p.state === 'closed')) {
		return [
			{
				kind: 'not-done',
				text: 'task not marked done',
				note: 'a pull request closed without a merge leaves its task open'
			}
		];
	}
	return [];
}
