import { expect, test } from 'vitest';
import { formatDuration, formatTime, intervalAnchor, intervalDuration } from './time';

test('formatTime shows the second and the zone', () => {
	expect(formatTime('2026-03-04T15:01:10Z')).toBe('2026-03-04 15:01:10 UTC');
	expect(formatTime('2026-03-04T15:01:10Z', 'America/Toronto')).toBe('2026-03-04 10:01:10 EST');
});

test('a value that is not a time, or an unknown zone, does not throw', () => {
	expect(formatTime('yesterday')).toBe('yesterday');
	expect(formatTime('2026-03-04T15:01:10Z', 'Not/A_Zone')).toBe('2026-03-04 15:01:10 UTC');
});

test('a duration only when both ends are known', () => {
	const start = { state: 'known' as const, at: '2026-03-05T13:02:35Z' };
	const end = { state: 'known' as const, at: '2026-03-05T13:09:12Z' };
	expect(intervalDuration({ start, end })).toBe(397_000);
	expect(intervalDuration({ start, end: { state: 'open' } })).toBeUndefined();
	expect(intervalDuration({ start: { state: 'unknown' }, end })).toBeUndefined();
	expect(
		intervalDuration({ start: { state: 'unknown' }, end: { state: 'unknown' } })
	).toBeUndefined();
});

test('the anchor is the start, else the end', () => {
	const at = '2026-03-05T13:09:12Z';
	expect(intervalAnchor({ start: { state: 'known', at }, end: { state: 'open' } })).toBe(at);
	expect(intervalAnchor({ start: { state: 'unknown' }, end: { state: 'known', at } })).toBe(at);
	expect(
		intervalAnchor({ start: { state: 'unknown' }, end: { state: 'unknown' } })
	).toBeUndefined();
});

test.each([
	[0, '0 s'],
	[45_000, '45 s'],
	[397_000, '6 min 37 s'],
	[3_605_000, '1 h 0 min 5 s'],
	[90_061_000, '1 d 1 h 1 min 1 s']
])('formatDuration(%d) is %s', (ms, want) => {
	expect(formatDuration(ms)).toBe(want);
});
