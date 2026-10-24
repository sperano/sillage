// Where a row's history is unknown in the window: a gap, the time after a
// source's last read, a source never read or with no coverage at all. The
// overview hatches these spans; what happened there is unknown, never idle.

import { coverageKey } from '../model';
import { unknownSpans, type UnknownSpan } from '../timeline/coverage';
import type { OverviewRow } from './model';
import type { Range } from './spans';

/**
 * The coverage keys of the sources a row reads: its coverage's, and those
 * its records come from, as the Go overview's rowSources.
 */
export function rowSources(row: OverviewRow): string[] {
	const keys = new Set(row.coverage.map((c) => coverageKey(c.source, c.instance)));
	for (const r of row.runs) keys.add(coverageKey('kelos', r.key.cluster));
	for (const p of row.pullRequests) keys.add(coverageKey('github', p.key.instance));
	for (const w of row.reviewWaits) keys.add(coverageKey('github', w.pr.instance));
	for (const m of row.marks) keys.add(coverageKey(m.source, m.instance));
	return [...keys].sort();
}

/** The spans of the window in which one of the row's sources is unknown, each with its reason. */
export function rowUnknownSpans(row: OverviewRow, range: Range): UnknownSpan[] {
	return rowSources(row).flatMap((s) => unknownSpans(row.coverage, s, range, 'utc'));
}
