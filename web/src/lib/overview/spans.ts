// The intervals an overview row draws, and where each goes in the window.
// A run's spans are the single-task timeline's (runSpans); a pull request
// is drawn open from each opening to its merge or close; a review wait
// from its request to its answer. Nothing is drawn between two records.

import { boundTime, type BoundState, type Interval, type Outcome, type Run } from '../model';
import { prKeyString, runKeyString } from '../keys';
import { runSpans } from '../timeline/intervals';
import { formatDuration, formatFull } from '../timeline/time';
import type { Mark, MarkKind, OverviewPullRequest, OverviewRow, ReviewWait } from './model';
import { lastRead, runLastRead } from './state';

/** What an interval was. Each is drawn with its own text and fill. */
export type SpanKind = 'waiting' | 'running' | 'review' | 'pr';

export interface OverviewSpan {
	key: string;
	kind: SpanKind;
	/** Says what the interval is, whatever its colour. */
	text: string;
	/** Set exactly when the start is known. */
	start?: number;
	endState: BoundState;
	/** Set exactly when endState is known. */
	end?: number;
	/** For an open end: the last read of its source, up to which it is known to continue. */
	openUntil?: number;
	/** What its known end was: a run's exit. */
	exit?: string;
	/** The run's outcome, for a run's span. */
	outcome?: Outcome;
}

/** The spans of a run: waiting to start, then running (or queued). */
export function runBars(row: OverviewRow, run: Run): OverviewSpan[] {
	return runSpans(run, runLastRead(row, run)).map((s) => ({
		key: s.key,
		kind: s.phase === 'waiting' ? 'waiting' : 'running',
		text: s.text,
		start: s.start,
		endState: s.endState,
		end: s.end,
		openUntil: s.openUntil,
		exit: s.exit?.label,
		outcome: run.outcome
	}));
}

function fromInterval(
	key: string,
	kind: SpanKind,
	text: string,
	i: Interval,
	read: number | undefined
): OverviewSpan {
	const open = i.end.state === 'open';
	return {
		key,
		kind,
		text,
		start: boundTime(i.start),
		endState: i.end.state,
		end: boundTime(i.end),
		openUntil: open ? read : undefined
	};
}

/** The spans a pull request was open. */
export function prBars(row: OverviewRow, pr: OverviewPullRequest): OverviewSpan[] {
	const read = lastRead(row.coverage, 'github', pr.key.instance);
	return pr.periods.map((p, i) =>
		fromInterval(`pr:${prKeyString(pr.key)}:${i}`, 'pr', `PR #${pr.key.number} open`, p, read)
	);
}

/** Who a review was asked of: a reviewer, or a team. */
export function requestee(w: ReviewWait): string {
	return w.reviewer ?? (w.team === undefined ? '' : `team ${w.team}`);
}

/**
 * The span of a review wait. A wait whose pull request closed unanswered
 * keeps its unknown end: closed says when the request stopped waiting for
 * the chips, not that an answer came then.
 */
export function waitBar(row: OverviewRow, w: ReviewWait): OverviewSpan {
	const who = requestee(w);
	return fromInterval(
		`review:${w.request}`,
		'review',
		who ? `review requested: ${who}` : 'review requested',
		w.interval,
		lastRead(row.coverage, 'github', w.pr.instance)
	);
}

/** The time a span's bar ends: its end, or for an open end its source's last read. */
export function drawnEnd(s: OverviewSpan): number | undefined {
	return s.endState === 'open' ? s.openUntil : s.end;
}

/** A window in milliseconds. */
export interface Range {
	start: number;
	end: number;
}

/**
 * Where a span goes in the window, in fractions of its width. A span with
 * both a start and a drawn end is a bar from left to right; one with only
 * one of them is a stub at that end (stub names the unknown side); one
 * with neither, or outside the window, is not drawn. The window's end is
 * outside it, as is a span that ends at its start.
 */
export interface Placement {
	left: number;
	right: number;
	stub?: 'start' | 'end';
	/** The bar runs on past the window's start or end. */
	clippedStart: boolean;
	clippedEnd: boolean;
}

function fraction(t: number, w: Range): number {
	return (t - w.start) / (w.end - w.start);
}

export function place(s: OverviewSpan, w: Range): Placement | undefined {
	const start = s.start;
	const end = drawnEnd(s);
	const inside = (t: number) => t >= w.start && t < w.end;
	if (start !== undefined && end !== undefined) {
		// A span ending at the window's start is before it, unless it is an instant there.
		if (end < w.start || (end === w.start && start < end) || start >= w.end) return undefined;
		return {
			left: Math.max(0, fraction(start, w)),
			right: Math.min(1, fraction(end, w)),
			clippedStart: start < w.start,
			clippedEnd: end > w.end
		};
	}
	const known = start ?? end;
	if (known === undefined || !inside(known)) return undefined;
	const at = fraction(known, w);
	return {
		left: at,
		right: at,
		stub: start === undefined ? 'start' : 'end',
		clippedStart: false,
		clippedEnd: false
	};
}

/** The short text of a mark. */
export function markLabel(m: Mark): string {
	const pr = m.pr ? `PR #${m.pr.number} ` : '';
	switch (m.kind) {
		case 'task.done':
			return 'task marked done';
		case 'run.announced':
			return 'run announced';
		case 'pr.opened':
			return `${pr}opened`;
		case 'pr.merged':
			return `${pr}merged`;
		case 'pr.closed':
			return `${pr}closed, not merged`;
		case 'pr.reopened':
			return `${pr}reopened`;
		case 'pr.review_submitted':
			return `${pr}review submitted`;
	}
}

/** The glyph of a mark: its own for a completion, a merge and a close, a dot for any other. */
export function markGlyph(kind: MarkKind): string {
	switch (kind) {
		case 'task.done':
			return '✓';
		case 'pr.merged':
			return '◆';
		case 'pr.closed':
			return '✕';
		default:
			return '•';
	}
}

/** The run a start announcement names: by its pod, in its namespace when it states one. */
export function announcedRun(row: OverviewRow, m: Mark): Run | undefined {
	if (m.pod === undefined) return undefined;
	return row.runs.find(
		(r) => r.pod === m.pod && (m.namespace === undefined || m.namespace === r.key.namespace)
	);
}

/** The key of a run, for its lane. */
export function runLaneKey(run: Run): string {
	return `run:${runKeyString(run.key)}`;
}

/** The text written on a bar: what it was, and how a run ended when it did. */
export function barText(s: OverviewSpan): string {
	return s.exit === undefined ? s.text : `${s.text} · ${s.exit}`;
}

/** Everything a span's bar shows, in words: what it was and what is known of each end. */
export function describeSpan(s: OverviewSpan): string {
	const start = s.start === undefined ? 'start unknown' : `from ${formatFull(s.start, 'utc')}`;
	const end =
		s.endState === 'open'
			? s.openUntil === undefined
				? 'not ended at the last read'
				: `not ended at the last read, ${formatFull(s.openUntil, 'utc')}`
			: s.end === undefined
				? 'end unknown'
				: `to ${formatFull(s.end, 'utc')}`;
	const length =
		s.start !== undefined && s.end !== undefined ? ` · ${formatDuration(s.end - s.start)}` : '';
	const exit = s.exit ? `, ${s.exit}` : '';
	return `${s.text}${length}, ${start}, ${end}${exit}`;
}

/** A mark in words: what changed, and when. */
export function describeMark(m: Mark): string {
	return `${markLabel(m)}, ${formatFull(Date.parse(m.at), 'utc')}`;
}
