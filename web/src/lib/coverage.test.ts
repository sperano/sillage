import { expect, test } from 'vitest';
import { goldenStories, goldenStory } from './testing/stories';
import { uncoveredSources } from './coverage';

test.each([...goldenStories])('%s: every source of a record has coverage', (_, story) => {
	expect(uncoveredSources(story)).toEqual([]);
});

test('a source with records and no coverage record is reported', () => {
	const story = goldenStory('implementation-merged');
	story.coverage = story.coverage.filter((c) => c.source === 'vikunja');
	expect(uncoveredSources(story)).toEqual([
		{ source: 'github', instance: 'github.example.com' },
		{ source: 'kelos', instance: 'cluster-a' }
	]);
});
