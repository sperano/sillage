import { describe, expect, test } from 'vitest';
import {
	DAY_MS,
	HOUR_MS,
	MINUTE_MS,
	formatClock,
	formatDuration,
	formatFull,
	tickStep,
	ticks
} from './time';

const AT = Date.parse('2026-03-03T18:05:40Z');

describe('formatting', () => {
	test('shows UTC times in UTC', () => {
		expect(formatClock(AT, 'utc')).toBe('18:05:40');
		expect(formatFull(AT, 'utc')).toBe('2026-03-03 18:05:40 UTC');
	});

	test('shows local times in the zone the browser runs in', () => {
		const local = new Date(AT);
		const pad = (n: number) => String(n).padStart(2, '0');
		expect(formatClock(AT, 'local')).toBe(
			`${pad(local.getHours())}:${pad(local.getMinutes())}:${pad(local.getSeconds())}`
		);
	});

	test.each([
		[45_000, '45s'],
		[6 * MINUTE_MS + 37_000, '6m 37s'],
		[HOUR_MS + 5 * MINUTE_MS, '1h 05m'],
		[2 * DAY_MS + 3 * HOUR_MS, '2d 3h']
	])('formats %i ms as %s', (ms, text) => {
		expect(formatDuration(ms)).toBe(text);
	});
});

describe('ticks', () => {
	test('picks the smallest step that fits', () => {
		expect(tickStep(10 * MINUTE_MS, 10)).toBe(MINUTE_MS);
		expect(tickStep(10 * HOUR_MS, 10)).toBe(HOUR_MS);
		expect(tickStep(365 * DAY_MS, 10)).toBe(30 * DAY_MS);
	});

	test('aligns on the zone wall clock and dates the first tick and each new day', () => {
		const start = Date.parse('2026-03-03T22:10:00Z');
		const end = Date.parse('2026-03-04T02:00:00Z');
		const out = ticks(start, end, 4, 'utc');
		expect(out.map((t) => t.label)).toEqual([
			'2026-03-03 23:00',
			'2026-03-04 00:00',
			'01:00',
			'02:00'
		]);
	});

	test('has no ticks for a window that is not a time, and caps a long one', () => {
		expect(ticks(Number.NaN, Number.NaN, 4, 'utc')).toEqual([]);
		expect(ticks(0, 100 * 365 * DAY_MS, 4, 'utc')).toHaveLength(4);
	});

	test('labels day steps with dates only', () => {
		const out = ticks(
			Date.parse('2026-01-01T12:00:00Z'),
			Date.parse('2026-01-04T00:00:00Z'),
			3,
			'utc'
		);
		expect(out.map((t) => t.label)).toEqual(['2026-01-02', '2026-01-03', '2026-01-04']);
	});
});
