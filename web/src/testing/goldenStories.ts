// The golden stories the Go model tests check (testdata/fixtures/stories),
// for the web tests to decode and render: one corpus for both sides.

/** Where the stories are, relative to the repository root. */
export const STORIES_DIR = 'testdata/fixtures/stories';

const files = import.meta.glob<unknown>('../../../testdata/fixtures/stories/*/story.golden.json', {
	eager: true,
	import: 'default'
});

// storyName returns a golden file's story: its directory's name.
function storyName(path: string): string {
	const name = path.split('/').at(-2);
	if (name === undefined) throw new Error(`golden story path ${path} has no directory`);
	return name;
}

/** Each golden story's JSON, by story name, in name order. */
export const goldenStories: ReadonlyMap<string, unknown> = new Map(
	Object.entries(files)
		.map(([path, json]): [string, unknown] => [storyName(path), json])
		.sort(([a], [b]) => a.localeCompare(b))
);

/** A copy of the golden story named name, free to modify; throws if there is none. */
export function goldenStory(name: string): unknown {
	const story = goldenStories.get(name);
	if (story === undefined) throw new Error(`no golden story ${name} in ${STORIES_DIR}`);
	return structuredClone(story);
}
