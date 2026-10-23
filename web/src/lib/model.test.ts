import { expect, test } from 'vitest';
import { boundTime, eventTime } from './model';
import { event } from './testing/builders';

test('an event sits at its source time, else its observed time', () => {
	expect(eventTime(event({ sourceTime: '2026-03-01T10:00:00Z' }))).toBe(
		Date.parse('2026-03-01T10:00:00Z')
	);
	expect(eventTime(event({ sourceTime: undefined, observedAt: '2026-03-01T11:00:00Z' }))).toBe(
		Date.parse('2026-03-01T11:00:00Z')
	);
});

test('only a known bound has a time', () => {
	expect(boundTime({ state: 'known', at: '2026-03-01T10:00:00Z' })).toBe(
		Date.parse('2026-03-01T10:00:00Z')
	);
	expect(boundTime({ state: 'open' })).toBeUndefined();
	expect(boundTime({ state: 'unknown' })).toBeUndefined();
});
