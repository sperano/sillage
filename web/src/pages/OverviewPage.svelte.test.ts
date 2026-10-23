import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { API_BASE } from '../lib/api';
import { memoryHistory } from '../lib/history';
import { DEFAULT_QUERY, overviewHref, type OverviewQuery } from '../lib/overview/query';
import { Router } from '../lib/router.svelte';
import { afternoon, overviewGoldenJSON } from '../lib/testing/overview';
import { fakeServer, json, problem } from '../testing/fakeServer';
import OverviewPage from './OverviewPage.svelte';

const OVERVIEW = `${API_BASE}/overview`;

async function renderOverview(
	query: OverviewQuery,
	handlers: Parameters<typeof fakeServer>[0] = { [OVERVIEW]: () => json(overviewGoldenJSON()) }
) {
	const history = memoryHistory(overviewHref(query));
	const router = new Router(history);
	const server = fakeServer(handlers);
	await render(OverviewPage, { api: server.api, router, query });
	return { history, server };
}

/** Moves the cursor's slider to a time of the fixture afternoon, as a reader would. */
async function moveCursor(clock: string) {
	const slider = page.getByLabelText('Cursor, UTC');
	await expect.element(slider).toBeVisible();
	const input = slider.element() as HTMLInputElement;
	input.value = String(afternoon(clock));
	input.dispatchEvent(new Event('input', { bubbles: true }));
}

test('shows a row per task, each linking to its story', async () => {
	await renderOverview(DEFAULT_QUERY);
	await expect
		.element(page.getByText(/2026-03-10 14:00 to 2026-03-10 15:00 UTC: 6 tasks/))
		.toBeVisible();
	const tasks = page.getByRole('list', { name: 'Tasks' });
	await expect
		.element(tasks.getByRole('link', { name: 'Health check' }))
		.toHaveAttribute('href', '/task/vikunja.example.com/201');
	expect(tasks.getByRole('listitem').all().length).toBeGreaterThanOrEqual(6);
});

test('says which tasks were executing at the cursor, and which are unknown then', async () => {
	await renderOverview(DEFAULT_QUERY);
	await moveCursor('14:25');
	const card = page.getByRole('region', { name: 'At the cursor' });
	await expect.element(card).toHaveTextContent('At the cursor · 14:25 UTC');
	await expect.element(card).toHaveTextContent('3 runs executing');
	await expect.element(card).toHaveTextContent('Health check, Cache metrics, Route rules');
	await expect.element(card).toHaveTextContent('Unknown then: Storage cleanup');
});

test('a pull request closed without a merge leaves its task open', async () => {
	await renderOverview(DEFAULT_QUERY);
	const state = page.getByRole('list', { name: "State of Route rules at the window's end" });
	await expect.element(state).toHaveTextContent('PR #23 closed, not merged');
	await expect.element(state).toHaveTextContent('task not marked done');
	await expect.element(state).not.toHaveTextContent('task marked done');
});

test('a row with missing history says so and measures nothing', async () => {
	await renderOverview(DEFAULT_QUERY);
	const state = page.getByRole('list', { name: "State of Storage cleanup at the window's end" });
	await expect.element(state).toHaveTextContent('history incomplete');
	await expect
		.element(page.getByRole('region', { name: 'Coverage' }))
		.toHaveTextContent('1 task with incomplete history');
	await expect.element(page.getByText(/idle/i).first()).toHaveTextContent('never counted as idle');
});

test('expanding a row labels each attempt, the failed one kept', async () => {
	await renderOverview(DEFAULT_QUERY);
	await expect.element(page.getByText('agent-cedar · attempt 1 · failed')).not.toBeInTheDocument();
	await page.getByRole('button', { name: 'Expand Cache metrics' }).click();
	await expect.element(page.getByText('agent-cedar · attempt 1 · failed')).toBeVisible();
	await expect.element(page.getByText('agent-cedar · attempt 2 · running')).toBeVisible();
	await expect
		.element(page.getByRole('img', { name: /^running · 27m 00s, from 2026-03-10 14:08:00 UTC/ }))
		.toBeVisible();
});

test('grouped by executor, simultaneous runs of one profile stay distinct', async () => {
	await renderOverview({ ...DEFAULT_QUERY, group: 'executor' });
	const cedar = page.getByRole('list', { name: 'Runs of agent-cedar' });
	await expect
		.element(cedar.getByRole('link', { name: 'Cache metrics · attempt 1 · failed' }))
		.toBeVisible();
	await expect.element(cedar.getByRole('link', { name: 'Health check · succeeded' })).toBeVisible();
	expect(cedar.getByRole('listitem').all()).toHaveLength(3);
	await moveCursor('14:25');
	await expect
		.element(page.getByRole('region', { name: 'At the cursor' }))
		.toHaveTextContent('3 runs executing');
});

test('loads the next page for the first page window', async () => {
	const golden = overviewGoldenJSON() as { rows: unknown[]; next?: string };
	const rest = golden.rows.splice(2);
	const { server } = await renderOverview(DEFAULT_QUERY, {
		[OVERVIEW]: (url) =>
			url.searchParams.get('after') === 'cursor-1'
				? json({ ...golden, rows: rest })
				: json({ ...golden, next: 'cursor-1' })
	});
	await expect.element(page.getByText(/6 tasks, 2 loaded/)).toBeVisible();
	await page.getByRole('button', { name: 'Load more tasks' }).click();
	await expect.element(page.getByRole('link', { name: 'Storage cleanup' })).toBeVisible();
	expect(server.requests.map((r) => r.path)).toEqual([
		OVERVIEW,
		`${OVERVIEW}?from=2026-03-10T14%3A00%3A00Z&to=2026-03-10T15%3A00%3A00Z&after=cursor-1`
	]);
});

test('applying filters puts them in the URL', async () => {
	const { history } = await renderOverview(DEFAULT_QUERY);
	await page.getByLabelText('Failed run').click();
	await page.getByRole('combobox', { name: 'Executor' }).fill('agent-cedar');
	await page.getByRole('button', { name: 'Apply' }).click();
	expect(history.entries().at(-1)).toBe('/overview?executor=agent-cedar&failed=true');
});

test('a refused window shows the API problem', async () => {
	await renderOverview(DEFAULT_QUERY, {
		[OVERVIEW]: () => problem(400, 'Bad Request', 'the window is longer than the 31 days allowed')
	});
	await expect.element(page.getByRole('alert')).toBeVisible();
});

test('an empty window says no task was active', async () => {
	const golden = overviewGoldenJSON() as object;
	await renderOverview(
		{ ...DEFAULT_QUERY, project: 'unknown' },
		{ [OVERVIEW]: () => json({ ...golden, rows: [], total: 0 }) }
	);
	await expect.element(page.getByText(/0 tasks matching the filters/)).toBeVisible();
	await expect
		.element(page.getByRole('region', { name: 'Tasks over time' }))
		.not.toBeInTheDocument();
});
