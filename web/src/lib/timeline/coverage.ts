// Where a source's records are unknown: its gaps, everything after its last
// successful read, and everything when it was never read. The timeline
// shades these spans with their reason; what happened in them is unknown,
// never idle time and never work.

import { coverageKey, type Story } from '../model';
import { formatFull, type Zone } from './time';
import type { TimeWindow } from './view';

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

/** Every source the story's coverage names, and every other one a row reads. */
export function storySources(story: Story, rowSources: string[][]): string[] {
	const keys = new Set(story.coverage.map((c) => coverageKey(c.source, c.instance)));
	for (const sources of rowSources) for (const s of sources) keys.add(s);
	return [...keys].sort();
}
