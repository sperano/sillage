// The intervals the timeline draws, each with what is known of its ends.
// Only an interval a record states is drawn: a run's lifecycle, a check, a
// review request until its answer. The time between two unrelated records
// is never one.

import {
	boundTime,
	eventKey,
	eventTime,
	prKeyString,
	runKeyString,
	type BoundState,
	type CheckRun,
	type Event,
	type PRKey,
	type Run
} from '../model';
import type { Selected } from '../selection.svelte';
import { checkEnd, runExitLabel, type MarkKind } from './marks';
import { formatDuration, formatFull, type Zone } from './time';

/**
 * What an interval was: waiting (a run queued or created but not started),
 * running, a review wait (a review requested and not yet answered), or a
 * check. Each is drawn with its own text and fill pattern.
 */
export type Phase = 'waiting' | 'running' | 'review-wait' | 'check';

export interface EndMark {
	mark: MarkKind;
	label: string;
	at: number;
}

export interface Span {
	selected: Selected;
	phase: Phase;
	/** Says what the interval is, whatever its colour. */
	text: string;
	startState: BoundState;
	/** Set exactly when startState is known. */
	start?: number;
	endState: BoundState;
	/** Set exactly when endState is known. */
	end?: number;
	/**
	 * For an open end: the last successful read of the interval's source,
	 * up to which it is known to continue. Unset when the source has none.
	 */
	openUntil?: number;
	/** The mark at a known end: a run's exit or a check's conclusion. */
	exit?: EndMark;
}

/** The spans of one run: its wait between creation and start, then its run. */
export function runSpans(run: Run, openUntil: number | undefined): Span[] {
	const selected: Selected = { kind: 'run', key: runKeyString(run.key) };
	const created = run.created === undefined ? undefined : Date.parse(run.created);
	const start = boundTime(run.interval.start);
	const end = boundTime(run.interval.end);
	const spans: Span[] = [];
	if (created !== undefined && start !== undefined && start > created) {
		spans.push({
			selected,
			phase: 'waiting',
			text: 'waiting to start',
			startState: 'known',
			start: created,
			endState: 'known',
			end: start
		});
	}
	const queued = run.outcome === 'waiting';
	// A run still queued has waited since its creation, when that is known.
	const from = queued && start === undefined ? created : start;
	spans.push({
		selected,
		phase: queued ? 'waiting' : 'running',
		text: queued ? 'waiting (queued)' : 'running',
		startState: from === undefined ? 'unknown' : 'known',
		start: from,
		endState: run.interval.end.state,
		end,
		openUntil: run.interval.end.state === 'open' ? openUntil : undefined,
		exit:
			end === undefined
				? undefined
				: { mark: 'run-exit', label: runExitLabel(run.outcome), at: end }
	});
	return spans;
}

/** The span of one check run, ending in its conclusion when it completed. */
export function checkSpan(pr: PRKey, check: CheckRun, openUntil: number | undefined): Span {
	const end = boundTime(check.interval.end);
	return {
		selected: { kind: 'check', pr: prKeyString(pr), id: check.id },
		phase: 'check',
		text: `check ${check.name}`,
		startState: check.interval.start.state,
		start: boundTime(check.interval.start),
		endState: check.interval.end.state,
		end,
		openUntil: check.interval.end.state === 'open' ? openUntil : undefined,
		exit: end === undefined ? undefined : { ...checkEnd(check.name, check.conclusion), at: end }
	};
}

function requestee(e: Event): string {
	return e.details?.reviewer ?? (e.details?.team ? `team ${e.details.team}` : '');
}

/** Whether later answers request: the reviewer's review, or its removal. */
function answers(request: Event, later: Event): boolean {
	if (later.kind === 'pr.review_request_removed') {
		return requestee(later) === requestee(request);
	}
	const reviewer = request.details?.reviewer;
	return (
		later.kind === 'pr.review_submitted' && reviewer !== undefined && later.actor.login === reviewer
	);
}

/**
 * The review waits of one pull request's events: from each review request
 * to the requested reviewer's review or the request's removal. With no
 * answer, the wait is open at the last read, unless the pull request was
 * merged or closed since, which ends the wait at a time no record states.
 */
export function reviewWaits(events: Event[], openUntil: number | undefined): Span[] {
	const sorted = [...events].sort((a, b) => eventTime(a) - eventTime(b));
	return sorted.flatMap((request, i) => {
		if (request.kind !== 'pr.review_requested') return [];
		const later = sorted.slice(i + 1);
		const answer = later.find((e) => answers(request, e));
		const ended = later.some((e) => e.kind === 'pr.merged' || e.kind === 'pr.closed');
		const who = requestee(request);
		const span: Span = {
			selected: { kind: 'review-wait', key: eventKey(request) },
			phase: 'review-wait',
			text: who ? `review wait: ${who}` : 'review wait',
			startState: 'known',
			start: eventTime(request),
			endState: answer ? 'known' : ended || openUntil === undefined ? 'unknown' : 'open',
			end: answer ? eventTime(answer) : undefined
		};
		if (span.endState === 'open') span.openUntil = openUntil;
		return [span];
	});
}

/** The bar's text: what the interval is and, when both ends are known, its length. */
export function spanText(span: Span): string {
	if (span.start !== undefined && span.end !== undefined) {
		return `${span.text} · ${formatDuration(span.end - span.start)}`;
	}
	return span.text;
}

/** Everything the bar shows, in words: what it is and what is known of each end. */
export function spanDescription(span: Span, zone: Zone): string {
	const start = span.start === undefined ? 'start unknown' : `from ${formatFull(span.start, zone)}`;
	const end =
		span.endState === 'open'
			? span.openUntil === undefined
				? 'not ended at the last read'
				: `not ended at the last read, ${formatFull(span.openUntil, zone)}`
			: span.end === undefined
				? 'end unknown'
				: `to ${formatFull(span.end, zone)}`;
	const exit = span.exit ? `, ${span.exit.label}` : '';
	return `${spanText(span)}, ${start}, ${end}${exit}`;
}
