// The state of a row at the window's end, as short chips: what was still
// running or waiting, the pull requests' states, the task's completion, and
// whether the history is incomplete. A closed, unmerged pull request never
// reads as the task being done: the task's state is Vikunja's.

import { boundTime, type Interval, type Run } from '../model';
import { prKeyString } from '../keys';
import { formatFull } from '../timeline/time';
import type { OverviewRow, ReviewWait, TaskCompletion } from './model';
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
	| 'reopened'
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
	return end !== undefined ? end > t : i.end.state === 'open';
}

/** An optional RFC 3339 time, in milliseconds. */
function optionalTime(s: string | undefined): number | undefined {
	return s === undefined ? undefined : Date.parse(s);
}

/** Whether a run still queued at its last read was created before t: then it was waiting at t. */
function queuedAt(run: Run, t: number): boolean {
	const created = optionalTime(run.created);
	return (
		run.outcome === 'waiting' &&
		run.interval.end.state === 'open' &&
		created !== undefined &&
		created < t
	);
}

/** A review wait's state at an instant, when it had one. */
export type ReviewState = 'requested' | 'unknown';

const OPEN_NOTE = 'not ended at the last read';
const CLOSED_LATER_NOTE = 'its pull request closed later with no answer recorded';

/**
 * A review wait's state at t: requested when it started before t and by t
 * neither was answered (an open end counts as not answered) nor had its
 * pull request closed; unknown once its pull request closed with no answer
 * recorded; none otherwise.
 */
export function reviewStateAt(w: ReviewWait, t: number): ReviewState | undefined {
	if (goingAt(w.interval, t)) return 'requested';
	if (w.interval.end.state !== 'unknown') return undefined;
	const start = boundTime(w.interval.start);
	if (start === undefined || start >= t) return undefined;
	const closed = optionalTime(w.closed);
	return closed !== undefined && closed > t ? 'requested' : 'unknown';
}

/** What a requested chip says of its end: open at the last read, or closed later unanswered. */
function requestedNote(w: ReviewWait): string | undefined {
	if (w.interval.end.state === 'open') return OPEN_NOTE;
	return w.interval.end.state === 'unknown' ? CLOSED_LATER_NOTE : undefined;
}

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
	chips.push(...row.reviewWaits.flatMap((w) => reviewChips(w, to)));
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
	if (goingAt(run.interval, to) || queuedAt(run, to)) {
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

/** A review wait's chip at the window's end: requested, unknown, or none once answered. */
function reviewChips(w: ReviewWait, to: number): Chip[] {
	switch (reviewStateAt(w, to)) {
		case 'requested':
			return [
				{
					kind: 'review',
					text: reviewText(w),
					note: requestedNote(w)
				}
			];
		case 'unknown':
			return [
				{
					kind: 'unknown',
					text: `${reviewText(w)}, answer unknown`,
					note: 'its pull request closed with no answer recorded'
				}
			];
		case undefined:
			return [];
	}
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

function completionChips(c: TaskCompletion): Chip[] {
	switch (c.state) {
		case 'done':
			return [{ kind: 'done', text: 'task marked done' }];
		case 'reopened': {
			const { after, by } = c.reopened;
			const seenDone = after === c.doneAt ? '' : `, seen done until ${utc(after)}`;
			return [
				{
					kind: 'reopened',
					text: 'task reopened',
					note: `marked done ${utc(c.doneAt)}${seenDone}, seen not done ${utc(by)}: when it was reopened is not recorded`
				}
			];
		}
	}
}

/** A time of a row, as the overview shows it: in UTC. */
function utc(t: string): string {
	return formatFull(Date.parse(t), 'utc');
}

/**
 * The task's completion at the window's end when it changed in the window:
 * marked done, or reopened since, which Vikunja records no time for: the
 * note says between which reads. Otherwise, when a pull request was closed
 * without a merge and no completion was recorded, that the task was not
 * marked done: the close does not close it. With part of the row's history
 * unknown, a completion could be there: then it is unknown.
 */
function doneChips(row: OverviewRow): Chip[] {
	if (row.completion !== undefined) return completionChips(row.completion);
	if (!row.pullRequests.some((p) => p.state === 'closed')) return [];
	if (row.missingHistory) {
		return [
			{
				kind: 'unknown',
				text: 'task completion unknown',
				note: 'a pull request closed without a merge; the incomplete history could hold a completion'
			}
		];
	}
	return [
		{
			kind: 'not-done',
			text: 'task not marked done',
			note: 'a pull request closed without a merge leaves its task open'
		}
	];
}
