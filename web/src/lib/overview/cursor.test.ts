import { describe, expect, it } from 'vitest';
import { MINUTE_MS } from '../timeline/time';
import { lastCursor, snapCursor } from './cursor';

const SECOND_MS = 1000;
const HALF_MINUTE_MS = 30 * SECOND_MS;
const start = Date.parse('2026-03-10T14:00:30Z');

describe('cursor', () => {
	it('steps in minutes from the window start, as the slider does', () => {
		const range = { start, end: Date.parse('2026-03-10T15:00:00Z') };
		expect(snapCursor(range, start + 10 * MINUTE_MS + 20 * SECOND_MS)).toBe(start + 10 * MINUTE_MS);
		expect(lastCursor(range)).toBe(start + 58 * MINUTE_MS);
		expect(snapCursor(range, range.end)).toBe(lastCursor(range));
		expect(snapCursor(range, start - MINUTE_MS)).toBe(start);
	});

	it('stays at the start of a window shorter than a minute', () => {
		const range = { start, end: start + HALF_MINUTE_MS };
		expect(lastCursor(range)).toBe(start);
		expect(snapCursor(range, range.end)).toBe(start);
	});

	it('keeps the last minute of a window of whole minutes', () => {
		const range = { start: start - HALF_MINUTE_MS, end: start - HALF_MINUTE_MS + 60 * MINUTE_MS };
		expect(lastCursor(range)).toBe(range.end - MINUTE_MS);
	});
});
