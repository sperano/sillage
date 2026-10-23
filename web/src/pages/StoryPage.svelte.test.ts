import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { storyPath } from '../lib/api';
import { parseStory } from '../lib/decodeModel';
import type { Root } from '../lib/model';
import { HANG, fakeServer, json, problem } from '../testing/fakeServer';
import { goldenStories, goldenStory } from '../testing/goldenStories';
import StoryPage from './StoryPage.svelte';

const TASK: Root = { task: { instance: 'vikunja.example.com', id: 102 } };

test.each([...goldenStories.keys()])('renders the golden story %s', async (name) => {
	const story = parseStory(goldenStory(name));
	const server = fakeServer({ [storyPath(story.root)]: () => json(goldenStory(name)) });
	await render(StoryPage, { api: server.api, root: story.root });

	const plural = (n: number, noun: string) => `${n} ${noun}${n === 1 ? '' : 's'}`;
	const events = plural(story.events.length, 'event');
	const runs = plural(story.runs.length, 'run');
	const prs = plural(story.pullRequests.length, 'pull request');
	const correlations = plural(story.correlations.length, 'correlation');
	await expect
		.element(page.getByText(events))
		.toHaveTextContent(`${events}, ${runs}, ${prs} and ${correlations}.`);
	const sources = page.getByRole('region', { name: 'Sources' });
	for (const c of story.coverage) {
		await expect.element(sources.getByRole('heading', { name: c.instance })).toBeVisible();
	}
});

test('a root sillage has no story of says so', async () => {
	const server = fakeServer({ [storyPath(TASK)]: () => problem(404, 'Not Found') });
	await render(StoryPage, { api: server.api, root: TASK });
	await expect
		.element(page.getByRole('alert'))
		.toHaveTextContent(
			'Sillage has no history of task 102 on vikunja.example.com: it may not have been read yet.'
		);
});

test('a server error is shown, not retried', async () => {
	const server = fakeServer({ [storyPath(TASK)]: () => problem(503, 'Service Unavailable') });
	await render(StoryPage, { api: server.api, root: TASK });
	await expect
		.element(page.getByRole('alert'))
		.toHaveTextContent("Sillage's server answered with an error (503 Service Unavailable).");
	expect(server.requests).toHaveLength(1);
});

test('another root aborts the request for the previous one', async () => {
	const other: Root = { task: { instance: 'vikunja.example.com', id: 101 } };
	const server = fakeServer({
		[storyPath(TASK)]: () => HANG,
		[storyPath(other)]: () => json(goldenStory('discuss-no-pr'))
	});
	const { rerender } = await render(StoryPage, { api: server.api, root: TASK });
	await expect.element(page.getByRole('status')).toBeVisible();
	await rerender({ root: other });
	await expect.element(page.getByText('6 events')).toBeVisible();
	expect(server.requests.map((r) => [r.path, r.signal.aborted])).toEqual([
		[storyPath(TASK), true],
		[storyPath(other), false]
	]);
});
