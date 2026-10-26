import { afterEach, beforeEach, expect, test } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { storyPath } from '../lib/api';
import { parseStory } from '../lib/decodeModel';
import type { Root } from '../lib/model';
import { HANG, fakeServer, json, problem } from '../testing/fakeServer';
import { goldenStories, goldenStory } from '../testing/goldenStories';
import StoryPage from './StoryPage.svelte';

const TASK: Root = { task: { instance: 'vikunja.example.com', id: 102 } };

/** A desktop viewport: the default one is a phone's, too narrow for the graph's bars. */
const VIEWPORT = { width: 1280, height: 900 };
/** Vitest's default browser viewport, restored after each test. */
const DEFAULT_VIEWPORT = { width: 414, height: 896 };

beforeEach(async () => {
	await page.viewport(VIEWPORT.width, VIEWPORT.height);
});

afterEach(async () => {
	await page.viewport(DEFAULT_VIEWPORT.width, DEFAULT_VIEWPORT.height);
});

/** The page showing the golden story name, as the server answers it. */
async function show(name: string, links = {}) {
	const story = parseStory(goldenStory(name));
	const server = fakeServer({ [storyPath(story.root)]: () => json(goldenStory(name)) });
	await render(StoryPage, { api: server.api, root: story.root, links });
	return story;
}

const graph = () => page.getByRole('region', { name: 'Timeline' });
/** Clicks a run's running bar in the graph: its drawn bar, not its label beside it. */
async function clickRunBar(nth = 0) {
	const bar = graph()
		.getByRole('button', { name: /^running/ })
		.nth(nth);
	await expect.element(bar).toBeVisible();
	await userEvent.click(bar.element().querySelector('[data-bar]')!);
}

const detailHeading = () =>
	page.getByRole('region', { name: 'Detail' }).getByRole('heading', { level: 3 });

test.each([...goldenStories.keys()])('renders the golden story %s', async (name) => {
	const story = await show(name);

	const plural = (n: number, noun: string) => `${n} ${noun}${n === 1 ? '' : 's'}`;
	const events = plural(story.events.length, 'event');
	const runs = plural(story.runs.length, 'run');
	const prs = plural(story.pullRequests.length, 'pull request');
	const correlations = plural(story.correlations.length, 'correlation');
	await expect
		.element(page.getByText(events))
		.toHaveTextContent(`${events}, ${runs}, ${prs} and ${correlations}.`);
	await expect
		.element(graph().getByRole('group', { name: 'Lanes of the story over time' }))
		.toBeVisible();
	const list = page.getByRole('region', { name: 'Events' });
	expect(list.getByRole('button').elements().length).toBeGreaterThan(0);
	const coverage = page.getByRole('region', { name: 'Coverage' });
	for (const c of story.coverage) {
		await expect.element(coverage.getByText(c.instance, { exact: false }).first()).toBeVisible();
	}
	await expect
		.element(page.getByRole('region', { name: 'Detail' }))
		.toHaveTextContent('Select an event, a run, a check or a phase to see its details.');
});

test('selecting a run in the graph shows its detail, and its console link', async () => {
	await show('failed-run-retry', { kelosConsoles: { 'cluster-a': 'https://kelos.example.com' } });
	await clickRunBar();
	await expect.element(detailHeading()).toHaveTextContent(/^Run vikunja-widgets-agent-opus-104/);
	await expect
		.element(
			page.getByRole('region', { name: 'Detail' }).getByRole('link', { name: /Kelos console/ })
		)
		.toHaveAttribute(
			'href',
			'https://kelos.example.com/api/resources/tasks/agents/vikunja-widgets-agent-opus-104/logs'
		);
});

test('the list keeps its roving tabindex, and Escape in the graph clears the selection', async () => {
	await show('implementation-merged');
	const rows = page.getByRole('region', { name: 'Events' }).getByRole('button');
	await expect.element(rows.first()).toBeVisible();
	expect(rows.elements().filter((b) => b.tabIndex === 0)).toHaveLength(1);
	rows
		.elements()
		.find((b) => b.tabIndex === 0)
		?.focus();
	await userEvent.keyboard('{Home}');
	await expect.element(rows.nth(0)).toHaveAttribute('aria-current', 'true');
	await userEvent.keyboard('{ArrowDown}');
	await expect.element(rows.nth(1)).toHaveFocus();
	await expect.element(detailHeading()).toHaveTextContent('Seen in bucket Doing');

	await clickRunBar();
	await expect.element(detailHeading()).toHaveTextContent(/^Run /);
	await userEvent.keyboard('{Escape}');
	await expect
		.element(page.getByRole('region', { name: 'Detail' }))
		.toHaveTextContent('Select an event, a run, a check or a phase to see its details.');
});

test("the time zone chosen in the graph is the panels' too", async () => {
	await show('implementation-merged');
	await page.getByRole('button', { name: /Task created/ }).click();
	await expect.element(detailHeading()).toHaveTextContent('Task created');
	const detail = page.getByRole('region', { name: 'Detail' });
	await expect.element(detail).toHaveTextContent(/UTC/);
	await graph().getByRole('combobox', { name: 'Time zone' }).selectOptions('local');
	await expect.element(detail).not.toHaveTextContent(/UTC/);
});

test('says so when Sillage has no story of the root', async () => {
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
