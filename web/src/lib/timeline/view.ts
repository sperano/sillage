// The visible window of time: the whole story, zoomed down to minutes, and
// panned within the story.

import { boundTime, eventTime, type Bound, type Story } from '../model';
import { MINUTE_MS } from './time';

/** A span of time in milliseconds since the epoch. */
export interface TimeWindow {
	start: number;
	end: number;
}

/** The narrowest window a zoom reaches. */
export const MIN_SPAN_MS = MINUTE_MS;

/** The share of the story's span added on each side of the whole-story window. */
const DOMAIN_PAD_FRACTION = 0.04;

/** The factor one zoom step narrows or widens the window by. */
export const ZOOM_STEP = 2;

/** The share of the window one pan step moves it by. */
export const PAN_FRACTION = 0.25;

/**
 * Every time the story records: events, run creations and known bounds,
 * check bounds, and each source's coverage gaps and last successful read,
 * after which the story is unknown. A time that does not parse is left out.
 */
export function storyTimes(story: Story): number[] {
	return recordedTimes(story).filter(Number.isFinite);
}

function recordedTimes(story: Story): number[] {
	const times = story.events.map(eventTime);
	for (const run of story.runs) {
		if (run.created) times.push(Date.parse(run.created));
		times.push(...knownBounds(run.interval.start, run.interval.end));
	}
	for (const pr of story.pullRequests) {
		for (const check of pr.checks) {
			times.push(...knownBounds(check.interval.start, check.interval.end));
		}
	}
	for (const c of story.coverage) {
		if (c.lastSuccessfulRead) times.push(Date.parse(c.lastSuccessfulRead));
		for (const gap of c.gaps ?? []) times.push(Date.parse(gap.start), Date.parse(gap.end));
	}
	return times;
}

function knownBounds(...bounds: Bound[]): number[] {
	return bounds.map(boundTime).filter((t) => t !== undefined);
}

/**
 * The whole story's window: every time it records, padded on each side,
 * and at least MIN_SPAN_MS wide. A story with no time at all centres on now.
 */
export function storyDomain(story: Story, now = Date.now()): TimeWindow {
	return windowAround(storyTimes(story), now);
}

/**
 * The window of times, padded on each side, and at least MIN_SPAN_MS wide;
 * centred on now when there is none.
 */
export function windowAround(times: readonly number[], now = Date.now()): TimeWindow {
	// A loop, not Math.min(...times): spreading a long array overflows the stack.
	const first = times.reduce((a, b) => Math.min(a, b), times[0] ?? now);
	const last = times.reduce((a, b) => Math.max(a, b), times[0] ?? now);
	const pad = Math.max((last - first) * DOMAIN_PAD_FRACTION, MIN_SPAN_MS / 2);
	return { start: first - pad, end: last + pad };
}

/** The window moved inside the domain, keeping its span (at most the domain's). */
function clampTo(w: TimeWindow, domain: TimeWindow): TimeWindow {
	const span = Math.min(w.end - w.start, domain.end - domain.start);
	const start = Math.min(Math.max(w.start, domain.start), domain.end - span);
	return { start, end: start + span };
}

/**
 * The window scaled by factor around center (less than 1 zooms in), kept
 * between MIN_SPAN_MS and the domain's span and inside the domain.
 */
export function zoom(
	w: TimeWindow,
	factor: number,
	center: number,
	domain: TimeWindow
): TimeWindow {
	const domainSpan = domain.end - domain.start;
	const span = Math.min(
		Math.max((w.end - w.start) * factor, Math.min(MIN_SPAN_MS, domainSpan)),
		domainSpan
	);
	const ratio = w.end === w.start ? 0.5 : (center - w.start) / (w.end - w.start);
	const start = center - span * ratio;
	return clampTo({ start, end: start + span }, domain);
}

/** The window moved by delta milliseconds, kept inside the domain. */
export function pan(w: TimeWindow, delta: number, domain: TimeWindow): TimeWindow {
	return clampTo({ start: w.start + delta, end: w.end + delta }, domain);
}

/** Maps time onto the plot's horizontal pixels. */
export interface Scale {
	window: TimeWindow;
	left: number;
	right: number;
	x(t: number): number;
	time(x: number): number;
}

export function scaleOf(window: TimeWindow, left: number, right: number): Scale {
	const span = window.end - window.start || 1;
	const width = right - left;
	return {
		window,
		left,
		right,
		x: (t) => left + ((t - window.start) / span) * width,
		time: (x) => window.start + ((x - left) / width) * span
	};
}
