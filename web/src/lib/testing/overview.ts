// The golden overview the Go tests check (testdata/fixtures/overview), for
// the web tests to decode and render: the afternoon of eight fabricated
// tasks, 14:00 to 15:00 UTC on 2026-03-10, seven of them active in it.

import { parseOverviewPage } from '../overview/decode';
import type { OverviewPage, OverviewRow } from '../overview/model';
import golden from '../../../../testdata/fixtures/overview/overview.golden.json';

/** A copy of the golden overview's JSON, free to modify. */
export function overviewGoldenJSON(): unknown {
	return structuredClone(golden);
}

/** A copy of the golden overview, decoded. */
export function overviewGolden(): OverviewPage {
	return parseOverviewPage(structuredClone(golden));
}

/** A time of the fixture afternoon, "14:25", in milliseconds. */
export function afternoon(clock: string): number {
	return Date.parse(`2026-03-10T${clock}:00Z`);
}

/**
 * A time for a test to close a review wait's pull request unanswered at:
 * after every golden request, before the window's end.
 */
export const CLOSED_UNANSWERED = '2026-03-10T14:50:00Z';

/** The row of task id; throws when the page has none. */
export function rowOfTask(page: OverviewPage, id: number): OverviewRow {
	const row = page.rows.find((r) => r.root.task.id === id);
	if (!row) throw new Error(`no row of task ${id}`);
	return row;
}
