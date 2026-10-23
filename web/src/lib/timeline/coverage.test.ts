import { expect, test } from 'vitest';
import { story } from '../testing/builders';
import { golden } from '../testing/goldens';
import { storySources, unknownSpans } from './coverage';

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
