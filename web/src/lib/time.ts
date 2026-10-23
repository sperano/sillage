import type { Bound, Interval } from './model';

/** The zone every time is shown in unless the page picks another. */
export const DEFAULT_TIME_ZONE = 'UTC';

const MS_PER_SECOND = 1000;
const SECONDS_PER_MINUTE = 60;
const MINUTES_PER_HOUR = 60;
const HOURS_PER_DAY = 24;
const MS_PER_MINUTE = MS_PER_SECOND * SECONDS_PER_MINUTE;
const MS_PER_HOUR = MS_PER_MINUTE * MINUTES_PER_HOUR;
const MS_PER_DAY = MS_PER_HOUR * HOURS_PER_DAY;

const formatters = new Map<string, Intl.DateTimeFormat>();

function formatter(timeZone: string): Intl.DateTimeFormat {
	let f = formatters.get(timeZone);
	if (!f) {
		f = new Intl.DateTimeFormat('en-CA', {
			timeZone,
			year: 'numeric',
			month: '2-digit',
			day: '2-digit',
			hour: '2-digit',
			minute: '2-digit',
			second: '2-digit',
			hourCycle: 'h23',
			timeZoneName: 'short'
		});
		formatters.set(timeZone, f);
	}
	return f;
}

/** The time in milliseconds, or undefined for a string that is not one. */
export function parseTime(iso: string): number | undefined {
	const ms = Date.parse(iso);
	return Number.isNaN(ms) ? undefined : ms;
}

/**
 * The time to the second, in timeZone, with the zone's name: "2026-03-04
 * 15:01:10 UTC". A string that is not a time is shown as it is, and an
 * unknown zone falls back to the default, so that one bad value cannot
 * blank the page.
 */
export function formatTime(iso: string, timeZone: string = DEFAULT_TIME_ZONE): string {
	const ms = parseTime(iso);
	if (ms === undefined) {
		return iso;
	}
	let f: Intl.DateTimeFormat;
	try {
		f = formatter(timeZone);
	} catch {
		f = formatter(DEFAULT_TIME_ZONE);
	}
	const parts = Object.fromEntries(f.formatToParts(ms).map((p) => [p.type, p.value]));
	return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}:${parts.second} ${parts.timeZoneName}`;
}

/** The bound's time, only when a source recorded it. */
export function knownTime(b: Bound): string | undefined {
	return b.state === 'known' ? b.at : undefined;
}

/**
 * The interval's length in milliseconds, only when both ends are known: an
 * unknown or open end has no duration, and none may be substituted.
 */
export function intervalDuration(i: Interval): number | undefined {
	const start = knownTime(i.start);
	const end = knownTime(i.end);
	if (start === undefined || end === undefined) {
		return undefined;
	}
	const startMs = parseTime(start);
	const endMs = parseTime(end);
	return startMs === undefined || endMs === undefined ? undefined : endMs - startMs;
}

/** The interval's first known bound: its start, else its end. */
export function intervalAnchor(i: Interval): string | undefined {
	return knownTime(i.start) ?? knownTime(i.end);
}

/** A duration to the second: "1 d 2 h 3 min 4 s", leading zero units left out. */
export function formatDuration(ms: number): string {
	const units: [number, string][] = [
		[MS_PER_DAY, 'd'],
		[MS_PER_HOUR, 'h'],
		[MS_PER_MINUTE, 'min'],
		[MS_PER_SECOND, 's']
	];
	let rest = Math.round(ms / MS_PER_SECOND) * MS_PER_SECOND;
	const out: string[] = [];
	for (const [size, name] of units) {
		const n = Math.floor(rest / size);
		rest -= n * size;
		if (n > 0 || out.length > 0) {
			out.push(`${n} ${name}`);
		}
	}
	return out.length > 0 ? out.join(' ') : '0 s';
}
