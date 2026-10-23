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

	// vite.config.ts runs these tests in America/Toronto, five hours behind
	// UTC in March before its daylight saving change.
	test('shows local times in the zone the browser runs in', () => {
		expect(formatClock(AT, 'local')).toBe('13:05:40');
		expect(formatFull(AT, 'local')).toBe('2026-03-03 13:05:40 America/Toronto');
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
	});

	test('falls back to the largest step when none fits', () => {
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

	// In America/Toronto, 2026-03-08 02:00 EST is 03:00 EDT, and 2026-11-01
	// 02:00 EDT is 01:00 EST.
	const localTicks = (start: string, end: string, maxTicks: number) =>
		ticks(Date.parse(start), Date.parse(end), maxTicks, 'local');

	test('skips the wall time a spring-forward change skips', () => {
		const out = localTicks('2026-03-08T05:30:00Z', '2026-03-08T09:00:00Z', 4);
		expect(out.map((t) => t.label)).toEqual(['2026-03-08 01:00', '03:00', '04:00', '05:00']);
		expect(out.map((t) => new Date(t.at).toISOString())).toEqual([
			'2026-03-08T06:00:00.000Z',
			'2026-03-08T07:00:00.000Z',
			'2026-03-08T08:00:00.000Z',
			'2026-03-08T09:00:00.000Z'
		]);
	});

	test('puts no tick before a window that starts in the skipped hour', () => {
		const start = '2026-03-08T06:30:00Z';
		const out = localTicks(start, '2026-03-08T09:00:00Z', 4);
		expect(out.map((t) => t.label)).toEqual(['2026-03-08 03:00', '04:00', '05:00']);
		expect(out.every((t) => t.at >= Date.parse(start))).toBe(true);
	});

	test('ticks a wall time a fall-back change repeats once, at its first occurrence', () => {
		const out = localTicks('2026-11-01T04:30:00Z', '2026-11-01T09:00:00Z', 5);
		expect(out.map((t) => t.label)).toEqual(['2026-11-01 01:00', '02:00', '03:00', '04:00']);
		expect(new Date(out[0]!.at).toISOString()).toBe('2026-11-01T05:00:00.000Z');
		expect(new Date(out[1]!.at).toISOString()).toBe('2026-11-01T07:00:00.000Z');
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
