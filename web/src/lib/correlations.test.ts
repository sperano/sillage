import { expect, test } from 'vitest';
import { goldenStory } from './testing/stories';
import { linksOf } from './correlations';
import { eventKey } from './keys';

const story = goldenStory('wiki-rooted-request');

test('an ambiguous correlation lists every candidate', () => {
	const live = story.events.find((e) => e.details?.version === 'live');
	if (!live) throw new Error('no live edit');
	const links = linksOf(story, { kind: 'event', id: eventKey(live) });
	expect(links).toHaveLength(2);
	for (const l of links) {
		expect(l.correlation.confidence).toBe('heuristic');
		expect(l.candidates.map((c) => c.id)).toEqual([
			'cluster-a/agents/uid/00000000-0000-4000-8000-000000000057',
			'cluster-a/agents/uid/00000000-0000-4000-8000-000000000103'
		]);
	}
});

test('from a candidate run, the other candidate is listed too', () => {
	const links = linksOf(story, {
		kind: 'run',
		id: 'cluster-a/agents/uid/00000000-0000-4000-8000-000000000103'
	});
	expect(links).toHaveLength(1);
	expect(links[0]?.candidates).toHaveLength(2);
});

test('a correlation is seen from both ends; an unambiguous one has no candidates', () => {
	const run = 'cluster-a/agents/uid/00000000-0000-4000-8000-000000000057';
	const links = linksOf(story, { kind: 'run', id: run });
	expect(links.map((l) => l.other.kind).sort()).toEqual([
		'event',
		'event',
		'event',
		'event',
		'page'
	]);
	const announced = links.find((l) => l.other.id === 'wiki/wiki.example.com/comments/881');
	expect(announced?.correlation.confidence).toBe('identifier');
	expect(announced?.candidates).toEqual([]);
});
