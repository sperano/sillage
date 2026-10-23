import { expect, test } from 'vitest';
import { goldenStories, goldenStory } from './testing/stories';
import { attributionUncertain } from './attribution';

const story = goldenStory('implementation-merged');

function of(id: string) {
	const e = story.events.find((x) => x.id === id);
	if (!e) throw new Error(`no event ${id}`);
	return attributionUncertain(story, e);
}

test('an account that acted for an agent is uncertain where no executor is known', () => {
	expect(of('reviews/3001')).toBe('owner-gh also acted for agent-opus in this story');
	expect(of('issues/events/7001')).toBe('owner-gh also acted for agent-opus in this story');
});

test('a known executor is certain', () => {
	expect(of('pulls/7')).toBeUndefined();
});

test('another source with the same login is another account', () => {
	const s = structuredClone(story);
	const created = s.events.find((x) => x.id === 'tasks/102');
	if (!created) throw new Error('no event tasks/102');
	created.actor = { login: 'owner-gh', kind: 'person' };
	created.instance = 'github.example.com';
	expect(attributionUncertain(s, created)).toBeUndefined();
});

test.each([...goldenStories].filter(([name]) => name !== 'implementation-merged'))(
	'%s: no executor anywhere, nothing uncertain',
	(_, s) => {
		expect(s.events.map((e) => attributionUncertain(s, e)).filter(Boolean)).toEqual([]);
	}
);
