import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { API_BASE } from '../lib/api';
import { memoryHistory } from '../lib/history';
import { DEFAULT_QUERY, overviewHref, type OverviewQuery } from '../lib/overview/query';
import { Router } from '../lib/router.svelte';
import { afternoon, overviewGoldenJSON } from '../lib/testing/overview';
import { fakeServer, HANG, json, problem } from '../testing/fakeServer';
import OverviewPage from './OverviewPage.svelte';

const OVERVIEW = `${API_BASE}/overview`;

/** The golden rows' titles, in their order. */
const TITLES = [
	'Health check',
	'Docs navigation',
	'Cache metrics',
	'Route rules',
	'Login banner',
	'Storage cleanup'
];

async function renderOverview(
	query: OverviewQuery,
	handlers: Parameters<typeof fakeServer>[0] = { [OVERVIEW]: () => json(overviewGoldenJSON()) }
) {
	const history = memoryHistory(overviewHref(query));
	const router = new Router(history);
	const server = fakeServer(handlers);
	const { rerender } = await render(OverviewPage, { api: server.api, router, query });
	return { history, server, rerender };
}

/** The golden overview's JSON, as a page whose next page is after cursor-1. */
function firstOfTwo(): { rows: unknown[]; next: string } {
	return { ...(overviewGoldenJSON() as { rows: unknown[] }), next: 'cursor-1' };
}

/** Moves the cursor's slider to a time of the fixture afternoon, as a reader would. */
async function moveCursor(clock: string) {
	const slider = page.getByLabelText('Cursor, UTC');
	await expect.element(slider).toBeVisible();
	// A range input takes no typing: set its value and fire input, as dragging does.
	const input = slider.element();
	if (!(input instanceof HTMLInputElement)) throw new Error('the cursor is not an input');
	input.value = String(afternoon(clock));
	input.dispatchEvent(new Event('input', { bubbles: true }));
}

test('shows a row per task, each linking to its story', async () => {
	await renderOverview(DEFAULT_QUERY);
	await expect
		.element(page.getByText(/2026-03-10 14:00 to 2026-03-10 15:00 UTC: 6 tasks/))
		.toBeVisible();
	const tasks = page.getByRole('list', { name: 'Tasks' });
	for (const title of TITLES) {
		await expect.element(tasks.getByRole('link', { name: title, exact: true })).toBeVisible();
	}
	await expect
		.element(tasks.getByRole('link', { name: 'Health check' }))
		.toHaveAttribute('href', '/task/vikunja.example.com/201');
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

test('a click on a lane moves the cursor there', async () => {
	await renderOverview(DEFAULT_QUERY);
	const lane = page.getByRole('group', { name: 'runs, pull requests and review waits' }).first();
	await expect.element(lane).toBeVisible();
	const plot = lane.element();
	const box = plot.getBoundingClientRect();
	// 25 minutes into the hour-long window.
	const fraction = 25 / 60;
	plot.dispatchEvent(
		new MouseEvent('click', {
			bubbles: true,
			clientX: box.left + box.width * fraction,
			clientY: box.top + box.height / 2
		})
	);
	await expect
		.element(page.getByRole('region', { name: 'At the cursor' }))
		.toHaveTextContent('At the cursor · 14:25 UTC');
});

test('a pull request closed without a merge leaves its task open', async () => {
	await renderOverview(DEFAULT_QUERY);
	const state = page.getByRole('list', { name: "State of Route rules at the window's end" });
	await expect.element(state).toHaveTextContent('PR #23 closed, not merged');
	await expect.element(state).toHaveTextContent('task not marked done');
	await expect.element(state).not.toHaveTextContent('task marked done');
});

test('a review whose answer is unknown is named apart, not counted as requested', async () => {
	const body = overviewGoldenJSON() as {
		rows: { reviewWaits: { interval: { end: unknown } }[] }[];
	};
	body.rows[0]!.reviewWaits[0]!.interval.end = { state: 'unknown' };
	await renderOverview(DEFAULT_QUERY, { [OVERVIEW]: () => json(body) });
	const card = page.getByRole('region', { name: "At the window's end" });
	await expect.element(card).toHaveTextContent('1 task with a review requested');
	await expect.element(card).toHaveTextContent('Review answer unknown: Health check');
	const state = page.getByRole('list', { name: "State of Health check at the window's end" });
	await expect.element(state).toHaveTextContent('review requested: owner-gh, answer unknown');
});

test('with only unknown review answers, no request is said not to wait', async () => {
	const body = overviewGoldenJSON() as {
		rows: { reviewWaits: { interval: { end: unknown } }[] }[];
	};
	for (const row of body.rows) {
		for (const w of row.reviewWaits) w.interval.end = { state: 'unknown' };
	}
	await renderOverview(DEFAULT_QUERY, { [OVERVIEW]: () => json(body) });
	const card = page.getByRole('region', { name: "At the window's end" });
	await expect.element(card).toHaveTextContent('No review request was known to be waiting.');
	await expect
		.element(card)
		.toHaveTextContent('Review answer unknown: Health check, Docs navigation');
});

test('a row with missing history says so, is hatched, and measures nothing', async () => {
	await renderOverview(DEFAULT_QUERY);
	const state = page.getByRole('list', { name: "State of Storage cleanup at the window's end" });
	await expect.element(state).toHaveTextContent('history incomplete');
	await expect.element(state).not.toHaveTextContent(/idle/);
	await expect
		.element(page.getByRole('img', { name: /^kelos\/cluster-a: the Kelos watch expired/ }))
		.toBeInTheDocument();
	await expect
		.element(page.getByRole('region', { name: 'Coverage' }))
		.toHaveTextContent('1 task with incomplete history');
});

test('a row names its executors and says when a run names none', async () => {
	const golden = overviewGoldenJSON() as { rows: { runs: { agent?: string }[] }[] };
	delete golden.rows[2]!.runs[1]!.agent;
	await renderOverview(DEFAULT_QUERY, { [OVERVIEW]: () => json(golden) });
	await expect
		.element(page.getByRole('listitem').filter({ hasText: 'Cache metrics' }).first())
		.toHaveTextContent('agent-cedar, no executor known');
});

test('expanding a row labels each attempt, the failed one kept', async () => {
	await renderOverview(DEFAULT_QUERY);
	await expect.element(page.getByText('agent-cedar · attempt 1 · failed')).not.toBeInTheDocument();
	const toggle = page.getByRole('button', { name: 'Details of Cache metrics' });
	await expect.element(toggle).toHaveAttribute('aria-expanded', 'false');
	await toggle.click();
	await expect.element(toggle).toHaveAttribute('aria-expanded', 'true');
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
	const first = firstOfTwo();
	const rest = first.rows.splice(2);
	const { server } = await renderOverview(DEFAULT_QUERY, {
		[OVERVIEW]: (url) =>
			url.searchParams.get('after') === 'cursor-1'
				? json({ ...first, rows: rest, next: undefined })
				: json(first)
	});
	await expect.element(page.getByText(/6 tasks, 2 loaded/)).toBeVisible();
	await page.getByRole('button', { name: 'Load more tasks' }).click();
	await expect.element(page.getByRole('link', { name: 'Storage cleanup' })).toBeVisible();
	expect(server.requests.map((r) => r.path)).toEqual([
		OVERVIEW,
		`${OVERVIEW}?from=2026-03-10T14%3A00%3A00Z&to=2026-03-10T15%3A00%3A00Z&after=cursor-1`
	]);
});

test('a failed next page says so and can be asked again', async () => {
	let failures = 1;
	await renderOverview(DEFAULT_QUERY, {
		[OVERVIEW]: (url) => {
			if (url.searchParams.get('after') === null) return json(firstOfTwo());
			if (failures-- > 0) return problem(503, 'Service Unavailable', 'reading the store failed');
			return json({ ...firstOfTwo(), rows: [], next: undefined });
		}
	});
	const more = page.getByRole('button', { name: 'Load more tasks' });
	await more.click();
	await expect.element(page.getByRole('alert')).toBeVisible();
	await expect.element(more).toBeEnabled();
	await more.click();
	await expect.element(more).not.toBeInTheDocument();
});

test('new filters abort the next page in flight, and start idle', async () => {
	const { server, rerender } = await renderOverview(DEFAULT_QUERY, {
		[OVERVIEW]: (url) => (url.searchParams.get('after') === null ? json(firstOfTwo()) : HANG)
	});
	const more = page.getByRole('button', { name: 'Load more tasks' });
	await more.click();
	await expect.element(page.getByRole('button', { name: 'Loading more tasks…' })).toBeDisabled();
	await rerender({ query: { ...DEFAULT_QUERY, failed: true } });
	await expect.element(page.getByText(/6 tasks matching the filters/)).toBeVisible();
	await expect.element(more).toBeEnabled();
	const [, next, refiltered] = server.requests;
	expect(next?.path).toContain('after=cursor-1');
	expect(next?.signal.aborted).toBe(true);
	expect(refiltered?.path).toBe(`${OVERVIEW}?failed=true`);
});

test('a next page that failed for other filters is not reported for the new ones', async () => {
	const { rerender } = await renderOverview(DEFAULT_QUERY, {
		[OVERVIEW]: (url) =>
			url.searchParams.get('after') === null
				? json(firstOfTwo())
				: problem(503, 'Service Unavailable', 'reading the store failed')
	});
	await page.getByRole('button', { name: 'Load more tasks' }).click();
	await expect.element(page.getByRole('alert')).toBeVisible();
	await rerender({ query: { ...DEFAULT_QUERY, failed: true } });
	await expect.element(page.getByText(/6 tasks matching the filters/)).toBeVisible();
	await expect.element(page.getByRole('alert')).not.toBeInTheDocument();
	await expect.element(page.getByRole('button', { name: 'Load more tasks' })).toBeEnabled();
});

test('a task a later page repeats is shown once', async () => {
	await renderOverview(DEFAULT_QUERY, {
		[OVERVIEW]: (url) =>
			url.searchParams.get('after') === null
				? json(firstOfTwo())
				: json({ ...firstOfTwo(), rows: firstOfTwo().rows.slice(0, 1), next: undefined })
	});
	await page.getByRole('button', { name: 'Load more tasks' }).click();
	await expect
		.element(page.getByRole('button', { name: 'Load more tasks' }))
		.not.toBeInTheDocument();
	expect(page.getByRole('link', { name: 'Health check', exact: true }).all()).toHaveLength(1);
});

test('applying filters puts them in the URL', async () => {
	const { history } = await renderOverview(DEFAULT_QUERY);
	await page.getByLabelText('Failed run').click();
	await page.getByRole('combobox', { name: 'Executor' }).fill('agent-cedar');
	await page.getByLabelText('From').fill('2026-03-10T14:00');
	await page.getByRole('radio', { name: 'By executor' }).click();
	await page.getByRole('button', { name: 'Apply' }).click();
	expect(history.entries().at(-1)).toBe(
		'/overview?from=2026-03-10T14%3A00%3A00Z&executor=agent-cedar&failed=true&group=executor'
	);
});

test('a refused window shows the API problem', async () => {
	await renderOverview(DEFAULT_QUERY, {
		[OVERVIEW]: () => problem(400, 'Bad Request', 'the window is longer than the 31 days allowed')
	});
	await expect
		.element(page.getByRole('alert'))
		.toHaveTextContent('the window is longer than the 31 days allowed');
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
