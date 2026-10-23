import { describe, expect, test } from 'vitest';
import { golden } from '../testing/goldens';
import { MINUTE_MS } from './time';
import { MIN_SPAN_MS, pan, scaleOf, storyDomain, zoom } from './view';

const DOMAIN = { start: 0, end: 100 * MINUTE_MS };

describe('storyDomain', () => {
	test('spans every recorded time, the last read included, padded', () => {
		const d = storyDomain(golden('implementation-merged'));
		expect(d.start).toBeLessThan(Date.parse('2026-03-03T18:00:00Z'));
		expect(d.end).toBeGreaterThan(Date.parse('2026-03-03T20:00:00Z'));
	});

	test('leaves out a time that does not parse', () => {
		const s = golden('discuss-no-pr');
		const broken = { ...s, events: [{ ...s.events[0]!, sourceTime: 'not a time' }] };
		const d = storyDomain(broken);
		expect(Number.isFinite(d.start) && Number.isFinite(d.end)).toBe(true);
	});

	test('is never narrower than the minimum span', () => {
		const story = { ...golden('discuss-no-pr'), events: [], runs: [], coverage: [] };
		const d = storyDomain(story, 0);
		expect(d.end - d.start).toBeGreaterThanOrEqual(MIN_SPAN_MS);
	});
});

describe('zoom', () => {
	test('narrows around the centre, keeping its place in the window', () => {
		const w = zoom(DOMAIN, 0.5, 25 * MINUTE_MS, DOMAIN);
		expect(w.end - w.start).toBe(50 * MINUTE_MS);
		expect(w.start).toBe(12.5 * MINUTE_MS);
	});

	test('stops at a minute and at the whole story', () => {
		const tiny = zoom(DOMAIN, 0.0001, 50 * MINUTE_MS, DOMAIN);
		expect(tiny.end - tiny.start).toBe(MIN_SPAN_MS);
		expect(zoom(tiny, 1e6, 0, DOMAIN)).toEqual(DOMAIN);
	});
});

describe('pan', () => {
	test('moves the window and stops at the story edges', () => {
		const w = { start: 10 * MINUTE_MS, end: 20 * MINUTE_MS };
		expect(pan(w, 5 * MINUTE_MS, DOMAIN)).toEqual({ start: 15 * MINUTE_MS, end: 25 * MINUTE_MS });
		expect(pan(w, -50 * MINUTE_MS, DOMAIN)).toEqual({ start: 0, end: 10 * MINUTE_MS });
		expect(pan(w, 500 * MINUTE_MS, DOMAIN)).toEqual({ start: 90 * MINUTE_MS, end: DOMAIN.end });
	});
});

test('scale maps time to pixels and back', () => {
	const s = scaleOf(DOMAIN, 100, 300);
	expect(s.x(50 * MINUTE_MS)).toBe(200);
	expect(s.time(200)).toBe(50 * MINUTE_MS);
});
