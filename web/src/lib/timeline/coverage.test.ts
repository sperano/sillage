import { describe, expect, test } from 'vitest';
import { story } from '../testing/builders';
import { golden } from '../testing/goldens';
import { REASON_PAD_PX, reasonText, storySources, unknownSpans } from './coverage';
import { SMALL_CHAR_PX } from './layout';
import { scaleOf } from './view';

const DOMAIN = {
	start: Date.parse('2026-01-01T00:00:00Z'),
	end: Date.parse('2026-01-02T00:00:00Z')
};

test('a source with no coverage is unknown throughout', () => {
	expect(unknownSpans(story(), 'wiki/w', DOMAIN, 'utc')).toEqual([
		{ ...DOMAIN, reason: 'wiki/w: no coverage recorded' }
	]);
});

test('a source never read is unknown throughout', () => {
	const s = story({ coverage: [{ source: 'wiki', instance: 'w' }] });
	expect(unknownSpans(s, 'wiki/w', DOMAIN, 'utc')[0]!.reason).toBe(
		'wiki/w: never read successfully'
	);
});

test('gaps keep their reason, clipped to the window, and time after the last read is unknown', () => {
	const s = story({
		coverage: [
			{
				source: 'kelos',
				instance: 'c',
				lastSuccessfulRead: '2026-01-01T20:00:00Z',
				gaps: [
					{ start: '2025-12-31T00:00:00Z', end: '2026-01-01T02:00:00Z', reason: 'watch expired' }
				]
			}
		]
	});
	expect(unknownSpans(s, 'kelos/c', DOMAIN, 'utc')).toEqual([
		{
			start: DOMAIN.start,
			end: Date.parse('2026-01-01T02:00:00Z'),
			reason: 'kelos/c: watch expired'
		},
		{
			start: Date.parse('2026-01-01T20:00:00Z'),
			end: DOMAIN.end,
			reason: 'kelos/c: unknown after the last successful read, 2026-01-01 20:00:00 UTC'
		}
	]);
});

test('lists covered sources and those rows read', () => {
	expect(storySources(golden('wiki-rooted-request'), [['vikunja/v']])).toEqual([
		'kelos/cluster-a',
		'vikunja/v',
		'wiki/wiki.example.com'
	]);
});

describe('reasonText', () => {
	const MINUTE = 60_000;
	const T0 = DOMAIN.start;
	// One pixel per minute, the plot from 100 to 300.
	const scale = scaleOf({ start: T0, end: T0 + 200 * MINUTE }, 100, 300);
	const reason = 'kelos/c: gap';
	const width = reason.length * SMALL_CHAR_PX + 2 * REASON_PAD_PX;
	const span = (from: number, to: number) => ({
		start: T0 + from * MINUTE,
		end: T0 + to * MINUTE,
		reason
	});

	test('writes the reason inside the visible part of a span it fits in', () => {
		expect(reasonText(span(-50, 150), scale)).toEqual({
			x: 100 + REASON_PAD_PX,
			anchor: 'start',
			text: reason
		});
	});

	test('writes it before a narrow span that starts in the window', () => {
		expect(reasonText(span(150, 151), scale)).toEqual({
			x: 250 - REASON_PAD_PX,
			anchor: 'end',
			text: reason
		});
	});

	test('cuts it inside the span when more of it fits there than before it', () => {
		const long = { ...span(10, 150), reason: 'kelos/c: '.padEnd(60, 'x') };
		const text = reasonText(long, scale);
		expect(text).toMatchObject({ x: 110 + REASON_PAD_PX, anchor: 'start' });
		expect(text!.text.length).toBeGreaterThan(10);
	});

	test('cuts it inside a narrow span that starts before the window', () => {
		const text = reasonText(span(-50, width - 10), scale)?.text;
		expect(text).toMatch(/…$/);
	});

	test('writes nothing for a span outside the window', () => {
		expect(reasonText(span(250, 260), scale)).toBeUndefined();
		expect(reasonText(span(-60, -50), scale)).toBeUndefined();
	});
});
