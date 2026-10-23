// Times on the timeline: the selected zone, the axis ticks and the text of
// times and durations. Every lane shows the same zone.

/** The zone every time on the timeline is shown in. */
export type Zone = 'local' | 'utc';

export const SECOND_MS = 1000;
export const MINUTE_MS = 60 * SECOND_MS;
export const HOUR_MS = 60 * MINUTE_MS;
export const DAY_MS = 24 * HOUR_MS;

/** The tick spacings the axis chooses from, smallest first. */
const TICK_STEPS_MS = [
	MINUTE_MS,
	2 * MINUTE_MS,
	5 * MINUTE_MS,
	10 * MINUTE_MS,
	15 * MINUTE_MS,
	30 * MINUTE_MS,
	HOUR_MS,
	2 * HOUR_MS,
	3 * HOUR_MS,
	6 * HOUR_MS,
	12 * HOUR_MS,
	DAY_MS,
	2 * DAY_MS,
	7 * DAY_MS,
	14 * DAY_MS,
	30 * DAY_MS
];

const PAD_WIDTH = 2;

/** The largest offset any zone has from UTC, UTC+14. */
const MAX_ZONE_OFFSET_MS = 14 * HOUR_MS;

/** The zone's offset from UTC at t, in milliseconds. */
function offsetMs(t: number, zone: Zone): number {
	return zone === 'utc' ? 0 : -new Date(t).getTimezoneOffset() * MINUTE_MS;
}

interface Fields {
	year: number;
	month: number;
	day: number;
	hour: number;
	minute: number;
	second: number;
}

function fields(t: number, zone: Zone): Fields {
	// Shifting by the offset and reading UTC fields gives the zone's wall time.
	const d = new Date(t + offsetMs(t, zone));
	return {
		year: d.getUTCFullYear(),
		month: d.getUTCMonth() + 1,
		day: d.getUTCDate(),
		hour: d.getUTCHours(),
		minute: d.getUTCMinutes(),
		second: d.getUTCSeconds()
	};
}

function pad(n: number): string {
	return String(n).padStart(PAD_WIDTH, '0');
}

/** The date, as 2026-03-03. */
export function formatDate(t: number, zone: Zone): string {
	const f = fields(t, zone);
	return `${f.year}-${pad(f.month)}-${pad(f.day)}`;
}

/** The time of day to the minute, as 18:05. */
export function formatMinute(t: number, zone: Zone): string {
	const f = fields(t, zone);
	return `${pad(f.hour)}:${pad(f.minute)}`;
}

/** The time of day to the second, as 18:05:40. */
export function formatClock(t: number, zone: Zone): string {
	const f = fields(t, zone);
	return `${pad(f.hour)}:${pad(f.minute)}:${pad(f.second)}`;
}

/** The full time, as 2026-03-03 18:05:40, with the zone's name. */
export function formatFull(t: number, zone: Zone): string {
	return `${formatDate(t, zone)} ${formatClock(t, zone)} ${zoneName(zone)}`;
}

/** The zone's name: UTC, or the browser's IANA zone. */
export function zoneName(zone: Zone): string {
	return zone === 'utc' ? 'UTC' : Intl.DateTimeFormat().resolvedOptions().timeZone;
}

/** A duration as its two largest units: 45s, 6m 37s, 1h 05m, 2d 3h. */
export function formatDuration(ms: number): string {
	const total = Math.round(ms / SECOND_MS) * SECOND_MS;
	const days = Math.floor(total / DAY_MS);
	const hours = Math.floor((total % DAY_MS) / HOUR_MS);
	const minutes = Math.floor((total % HOUR_MS) / MINUTE_MS);
	const seconds = Math.floor((total % MINUTE_MS) / SECOND_MS);
	if (days > 0) return `${days}d ${hours}h`;
	if (hours > 0) return `${hours}h ${pad(minutes)}m`;
	if (minutes > 0) return `${minutes}m ${pad(seconds)}s`;
	return `${seconds}s`;
}

export interface Tick {
	at: number;
	label: string;
}

/** The smallest tick step that keeps the window to at most maxTicks ticks. */
export function tickStep(span: number, maxTicks: number): number {
	const step = TICK_STEPS_MS.find((s) => span / s <= maxTicks);
	return step ?? TICK_STEPS_MS[TICK_STEPS_MS.length - 1]!;
}

/**
 * The first instant at which the zone's wall clock reads wall (a wall time
 * written as if it were UTC), or undefined when a daylight saving change
 * skips it. The zone's offset a day either side covers both offsets of any
 * change near it; an instant is the wall time only under its own offset.
 */
function instantOf(wall: number, zone: Zone): number | undefined {
	const offsets = new Set([offsetMs(wall - DAY_MS, zone), offsetMs(wall + DAY_MS, zone)]);
	const instants = [...offsets]
		.map((o) => wall - o)
		.filter((at) => wall - at === offsetMs(at, zone));
	return instants.length > 0 ? Math.min(...instants) : undefined;
}

/**
 * The axis ticks in [start, end], aligned on the zone's wall clock: whole
 * minutes, hours or midnights there, and never more than maxTicks, even
 * past the largest step. A wall time a daylight saving change skips has no
 * tick, and one it repeats has one, at its first occurrence. A tick shows
 * its date when it is the first or the day changed since the previous one.
 * A window that is not a finite time has no ticks.
 */
export function ticks(start: number, end: number, maxTicks: number, zone: Zone): Tick[] {
	if (!Number.isFinite(start) || !Number.isFinite(end)) return [];
	const step = tickStep(end - start, maxTicks);
	const out: Tick[] = [];
	let wall = Math.ceil((start + offsetMs(start, zone)) / step) * step;
	let previousDay = '';
	// No instant of a wall time is earlier than the largest offset allows.
	for (; wall - MAX_ZONE_OFFSET_MS <= end && out.length < maxTicks; wall += step) {
		const at = instantOf(wall, zone);
		if (at === undefined || at < start) continue;
		if (at > end) break;
		const day = formatDate(at, zone);
		const label =
			step >= DAY_MS
				? day
				: day !== previousDay
					? `${day} ${formatMinute(at, zone)}`
					: formatMinute(at, zone);
		out.push({ at, label });
		previousDay = day;
	}
	return out;
}
