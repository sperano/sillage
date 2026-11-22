import { afterEach, beforeEach, expect, test } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { costPath, deliveriesPath, storyPath } from '../lib/api';
import { parseStory } from '../lib/decodeModel';
import type { Root } from '../lib/model';
import { goldenResponse } from '../lib/testing/apiResponses';
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
	expect(server.requests.filter((r) => r.path === storyPath(TASK))).toHaveLength(1);
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
	await expect.element(page.getByText('5 events')).toBeVisible();
	const asked = (path: (root: Root) => string) =>
		server.requests
			.filter((r) => r.path === path(TASK) || r.path === path(other))
			.map((r) => [r.path, r.signal.aborted]);
	expect(asked(storyPath)).toEqual([
		[storyPath(TASK), true],
		[storyPath(other), false]
	]);
	expect(asked(deliveriesPath)).toEqual([
		[deliveriesPath(TASK), true],
		[deliveriesPath(other), false]
	]);
	expect(asked(costPath)).toEqual([
		[costPath(TASK), true],
		[costPath(other), false]
	]);
});

/** The page showing implementation-merged, and the deliveries the Go handler answers for it. */
async function showWithDeliveries(deliveries = () => json(goldenResponse(deliveriesPath(TASK)))) {
	const server = fakeServer({
		[storyPath(TASK)]: () => json(goldenStory('implementation-merged')),
		[deliveriesPath(TASK)]: deliveries
	});
	await render(StoryPage, { api: server.api, root: TASK });
	await expect.element(page.getByText('9 events')).toBeVisible();
}

const sillageRow = () => graph().element().querySelector('[data-row="sillage:deliveries"]');

test('the Sillage lane counts the deliveries, and draws them once zoomed in', async () => {
	await showWithDeliveries();
	await expect
		.poll(() => sillageRow()?.querySelector('[data-note]')?.textContent)
		.toContain('7 deliveries · zoom in to see them');
	expect(sillageRow()?.querySelector('[data-delivery]')).toBeNull();
	await expect.element(page.getByText(/Sillage lane \(triangle\)/)).toBeVisible();

	await graph().getByRole('button', { name: 'Zoom in' }).click();
	await expect.poll(() => sillageRow()?.querySelector('[data-note]')).toBeNull();
	const mark = sillageRow()?.querySelector<SVGElement>('[data-delivery]');
	expect(mark).toBeTruthy();
	await userEvent.click(mark!);
	await expect.element(detailHeading()).toHaveTextContent(/^Webhook delivery: /);
	await expect
		.element(page.getByRole('region', { name: 'Detail' }))
		.toHaveTextContent(/not evidence that Kelos received its own delivery/);
});

test('a state change lists the deliveries received around it, each selectable', async () => {
	await showWithDeliveries();
	await page.getByRole('button', { name: /Pull request merged/ }).click();
	const detail = page.getByRole('region', { name: 'Detail' });
	await expect.element(detail).toHaveTextContent(/Webhook deliveries around it/);
	await expect.element(detail).toHaveTextContent(/none is linked to this change/);
	const listed = detail.getByRole('button', { name: /^GitHub|^Vikunja/ });
	await expect.element(listed.first()).toBeVisible();
	// The review, the merge's failed attempt and its redelivery, the task's
	// update and the review's repeat; not the PR's opening, an hour before.
	expect(listed.elements().map((b) => b.textContent)).toEqual([
		'GitHub pull_request_review',
		'GitHub pull_request',
		'GitHub pull_request',
		'Vikunja task.updated',
		'GitHub pull_request_review'
	]);
	await listed.nth(1).click();
	await expect.element(detailHeading()).toHaveTextContent('Webhook delivery: GitHub pull_request');
	await expect.element(detailHeading()).toHaveFocus();
	await expect
		.element(detail.getByTestId('delivery-outcome'))
		.toHaveTextContent(/^failed: sillage could not ask for every read/);
	await expect.element(detail.getByTestId('next-read')).toHaveTextContent(/2026-03-03 19:13:00/);
});

test('the story shows without its deliveries when they fail', async () => {
	await showWithDeliveries(() => problem(503, 'Service Unavailable'));
	await expect.element(page.getByText(/received are not shown/)).toBeVisible();
	expect(sillageRow()).toBeNull();
	await page.getByRole('button', { name: /Task marked done/ }).click();
	await expect
		.element(page.getByRole('region', { name: 'Detail' }))
		.toHaveTextContent(/What sillage received is unknown here/);
});

const COST_TASK: Root = { task: { instance: 'vikunja.example.com', id: 301 } };

/** The golden cost of COST_TASK, as the server would answer it for TASK. */
function costOfTask() {
	const body = goldenResponse(costPath(COST_TASK)) as { root: Root };
	body.root = TASK;
	return json(body);
}

/** The page showing implementation-merged, its cost answered by cost, at hash. */
async function showWithCost(cost: () => Response, hash = '') {
	const server = fakeServer({
		[storyPath(TASK)]: () => json(goldenStory('implementation-merged')),
		[costPath(TASK)]: cost
	});
	await render(StoryPage, { api: server.api, root: TASK, hash });
	await expect.element(page.getByText('9 events')).toBeVisible();
	return server;
}

const costSection = () => page.getByRole('region', { name: 'Estimated cost' });

test('the story shows its estimated cost below, in the section #cost', async () => {
	const server = await showWithCost(costOfTask);
	await expect
		.element(costSection().getByTestId('cost-total'))
		.toHaveTextContent(/^at least US\$18\.8864$/);
	await expect.element(costSection()).toHaveAttribute('id', 'cost');
	await expect.element(costSection()).not.toBeInViewport();
	expect(server.requests.filter((r) => r.path === costPath(TASK))).toHaveLength(1);
});

test('a link to #cost scrolls to the section once it is drawn', async () => {
	await showWithCost(costOfTask, '#cost');
	await expect.element(costSection().getByTestId('cost-total')).toBeVisible();
	await expect.element(costSection()).toBeInViewport();
});

test('the story shows without its cost when the cost fails, which is not retried', async () => {
	const server = await showWithCost(() => problem(503, 'Service Unavailable'));
	await expect
		.element(costSection().getByTestId('cost-failed'))
		.toHaveTextContent(
			"Sillage's server answered with an error (503 Service Unavailable). The estimated cost is not shown."
		);
	await expect.element(graph()).toBeVisible();
	expect(server.requests.filter((r) => r.path === costPath(TASK))).toHaveLength(1);
});
