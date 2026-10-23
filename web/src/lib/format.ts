// How times read on the page. Every time shows in UTC, labelled as such,
// until the reader can choose a timezone.

import type { Source } from './model';

/** A source's name as its users know it. */
export const SOURCE_NAMES: Readonly<Record<Source, string>> = {
	vikunja: 'Vikunja',
	kelos: 'Kelos',
	github: 'GitHub',
	wiki: 'Wiki.js'
};

export const MS_PER_SECOND = 1000;

// The parts of an ISO 8601 UTC time this shows: date, and time to the second.
const DATE_LENGTH = 'YYYY-MM-DD'.length;
const TIME_END = 'YYYY-MM-DDTHH:MM:SS'.length;

/** time, an RFC 3339 string, as "2026-03-03 20:00:00 UTC". */
export function formatTime(time: string): string {
	const iso = new Date(time).toISOString();
	return `${iso.slice(0, DATE_LENGTH)} ${iso.slice(DATE_LENGTH + 1, TIME_END)} UTC`;
}

/** n and the noun it counts: "1 run", "2 runs". */
export function count(n: number, singular: string, plural = `${singular}s`): string {
	return `${n} ${n === 1 ? singular : plural}`;
}
