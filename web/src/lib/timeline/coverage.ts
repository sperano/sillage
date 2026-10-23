// Where a source's records are unknown: its gaps, everything after its last
// successful read, and everything when it was never read. The timeline
// shades these spans with their reason; what happened in them is unknown,
// never idle time and never work.

import { coverageKey, type Story } from '../model';
import { fitText, SMALL_CHAR_PX } from './layout';
import { formatFull, type Zone } from './time';
import type { Scale, TimeWindow } from './view';

/** Room kept between a reason and its span's edge. */
export const REASON_PAD_PX = 4;

export interface UnknownSpan {
	start: number;
	end: number;
	reason: string;
}

/** The spans of domain in which the source's records are unknown. */
export function unknownSpans(
	story: Story,
	source: string,
	domain: TimeWindow,
	zone: Zone
): UnknownSpan[] {
	const c = story.coverage.find((c) => coverageKey(c.source, c.instance) === source);
	if (!c) return [{ ...domain, reason: `${source}: no coverage recorded` }];
	if (!c.lastSuccessfulRead) return [{ ...domain, reason: `${source}: never read successfully` }];
	const spans: UnknownSpan[] = (c.gaps ?? []).map((g) => ({
		start: Date.parse(g.start),
		end: Date.parse(g.end),
		reason: `${source}: ${g.reason}`
	}));
	const lastRead = Date.parse(c.lastSuccessfulRead);
	spans.push({
		start: lastRead,
		end: domain.end,
		reason: `${source}: unknown after the last successful read, ${formatFull(lastRead, zone)}`
	});
	return clip(spans, domain);
}

function clip(spans: UnknownSpan[], domain: TimeWindow): UnknownSpan[] {
	return spans
		.map((s) => ({
			...s,
			start: Math.max(s.start, domain.start),
			end: Math.min(s.end, domain.end)
		}))
		.filter((s) => s.end > s.start);
}

/** Where a span's reason is written, and the text written there. */
export interface ReasonText {
	x: number;
	anchor: 'start' | 'end';
	text: string;
}

/**
 * Where a span's reason goes: whole, inside the part of the span in the
 * window when it fits there; else cut, before the span (when it starts in
 * the window) or inside its visible part, wherever more of it fits, before
 * it on a tie. Undefined for a span outside the window, or with no room for
 * the reason at all.
 */
export function reasonText(u: UnknownSpan, scale: Scale): ReasonText | undefined {
	const x1 = scale.x(u.start);
	const from = Math.max(x1, scale.left);
	const visible = Math.min(scale.x(u.end), scale.right) - from;
	if (visible <= 0) return undefined;
	const fit = (width: number) => fitText(u.reason, width - 2 * REASON_PAD_PX, SMALL_CHAR_PX);
	if (fit(visible) === u.reason)
		return { x: from + REASON_PAD_PX, anchor: 'start', text: u.reason };
	const before = x1 > scale.left ? fit(x1 - scale.left) : '';
	const inside = fit(visible);
	if (before && before.length >= inside.length) {
		return { x: x1 - REASON_PAD_PX, anchor: 'end', text: before };
	}
	return inside ? { x: from + REASON_PAD_PX, anchor: 'start', text: inside } : undefined;
}

/** Every source the story's coverage names, and every other one a row reads. */
export function storySources(story: Story, rowSources: string[][]): string[] {
	const keys = new Set(story.coverage.map((c) => coverageKey(c.source, c.instance)));
	for (const sources of rowSources) for (const s of sources) keys.add(s);
	return [...keys].sort();
}
