// The fixture stories the Go tests check (testdata/fixtures/stories), shared
// with the web tests so that one corpus covers building and rendering.

import type { Story } from '../model';

const files = import.meta.glob<Story>('../../../../testdata/fixtures/stories/*/story.golden.json', {
	eager: true,
	import: 'default'
});

/** Every golden story by its directory name. */
export const goldens: ReadonlyMap<string, Story> = new Map(
	Object.entries(files).map(([path, story]) => [path.split('/').at(-2)!, story])
);

/** The golden story of that name; throws when there is none. */
export function golden(name: string): Story {
	const story = goldens.get(name);
	if (!story) throw new Error(`no golden story ${name}`);
	return story;
}

/** A deep copy of a story, for a test to change. */
export function copy(story: Story): Story {
	return structuredClone(story);
}
