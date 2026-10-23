import type { Story } from '../model';

// The golden stories the Go model tests check, which the web tests render.
// Test code only: nothing the app imports.

const files = import.meta.glob<Story>('../../../../testdata/fixtures/stories/*/story.golden.json', {
	eager: true,
	import: 'default'
});

const STORY_NAME = /\/stories\/([^/]+)\/story\.golden\.json$/;

export const goldenStories: ReadonlyMap<string, Story> = new Map(
	Object.entries(files).map(([path, story]) => [STORY_NAME.exec(path)?.[1] ?? path, story])
);

export function goldenStory(name: string): Story {
	const story = goldenStories.get(name);
	if (!story) {
		throw new Error(`no golden story ${name}`);
	}
	// A copy, so that a test changing it changes no other test's.
	return structuredClone(story);
}
