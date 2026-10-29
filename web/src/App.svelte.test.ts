import { afterEach, expect, test } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import App from './App.svelte';
import { API_BASE } from './lib/api';
import { memoryHistory } from './lib/history';
import { Router } from './lib/router.svelte';
import { HANG, fakeServer, json } from './testing/fakeServer';
import { goldenStory } from './testing/goldenStories';
import { goldenResponse } from './lib/testing/apiResponses';

const TASK_STORY = `${API_BASE}/stories/task/vikunja.example.com/102`;
const CONFIG = `${API_BASE}/config`;

/** A desktop viewport, wider than the narrow pages' column. */
const WIDE = { width: 1400, height: 900 };
/** Vitest's default browser viewport, restored after a test changes it. */
const DEFAULT_VIEWPORT = { width: 414, height: 896 };
/** The narrow pages' column: 60rem at the default 16px font size. */
const NARROW_PX = 960;
afterEach(async () => {
	await page.viewport(DEFAULT_VIEWPORT.width, DEFAULT_VIEWPORT.height);
});

async function renderAt(href: string, handlers: Parameters<typeof fakeServer>[0] = {}) {
	const history = memoryHistory(href);
	const server = fakeServer({
		[`${API_BASE}/coverage`]: () => json({ sources: [], off: [] }),
		...handlers
	});
	const router = new Router(history);
	await render(App, { api: server.api, router });
	return { history, server, router };
}

test('/ is the entry page', async () => {
	await renderAt('/');
	await expect.element(page.getByRole('heading', { level: 1 })).toHaveTextContent('Sillage');
	await expect.element(page.getByRole('search')).toBeVisible();
	await expect.element(page.getByRole('heading', { name: 'Open a link' })).toBeVisible();
	await expect.element(page.getByRole('heading', { name: 'Source freshness' })).toBeVisible();
});

test('a deep link asks for its story once', async () => {
	const { server } = await renderAt('/task/vikunja.example.com/102', { [TASK_STORY]: () => HANG });
	await expect.element(page.getByRole('status')).toHaveTextContent('Loading the history');
	expect(server.requests.filter((r) => r.path === TASK_STORY)).toHaveLength(1);
});

test('a search result opens its story in the page, and back returns to the results', async () => {
	const { history, server } = await renderAt('/?q=widgets', {
		[`${API_BASE}/search`]: () =>
			json({
				query: 'widgets',
				roots: [{ root: { task: { instance: 'vikunja.example.com', id: 102 } }, title: 'Widgets' }]
			}),
		[TASK_STORY]: () => json(goldenStory('implementation-merged'))
	});
	await page.getByRole('link', { name: 'Widgets' }).click();
	await expect
		.element(page.getByRole('heading', { level: 1 }))
		.toHaveTextContent('History of task 102 on vikunja.example.com');
	expect(history.entries()).toEqual(['/?q=widgets', '/task/vikunja.example.com/102']);
	expect(server.requests.map((r) => r.path)).toContain(TASK_STORY);
	await expect.element(page.getByRole('heading', { level: 1 })).toHaveFocus();
	expect(document.title).toBe('History of task 102 on vikunja.example.com · Sillage');

	history.back();
	await expect.element(page.getByRole('link', { name: 'Widgets' })).toBeVisible();
});

test('a new search keeps the focus in the search box', async () => {
	const { history } = await renderAt('/?q=widgets', {
		[`${API_BASE}/search`]: (url) => json({ query: url.searchParams.get('q'), roots: [] })
	});
	const box = page.getByRole('searchbox', { name: 'Title or key' });
	await userEvent.clear(box);
	await userEvent.type(box, 'gears{Enter}');
	expect(history.entries()).toEqual(['/?q=widgets', '/?q=gears']);
	await expect.element(page.getByRole('status')).toHaveTextContent('No task matches “gears”.');
	await expect.element(box).toHaveFocus();
});

test('leaving a story while it loads aborts its request', async () => {
	const { server } = await renderAt('/task/vikunja.example.com/102', { [TASK_STORY]: () => HANG });
	await expect.element(page.getByRole('status')).toHaveTextContent('Loading the history');
	await page.getByRole('link', { name: 'Sillage' }).click();
	await expect.element(page.getByRole('search')).toBeVisible();
	const story = server.requests.find((r) => r.path === TASK_STORY);
	expect(story?.signal.aborted).toBe(true);
});

test('/open resolves the link and shows its story in place of /open', async () => {
	const link = 'https://github.example.com/example-org/widgets/pull/7';
	const { history } = await renderAt(`/open?url=${encodeURIComponent(link)}`, {
		// What the Go handler answers for this link.
		[`${API_BASE}/resolve`]: (url) => json(goldenResponse(url.pathname + url.search)),
		[TASK_STORY]: () => json(goldenStory('implementation-merged'))
	});
	await expect
		.element(page.getByRole('heading', { level: 1 }))
		.toHaveTextContent('History of task 102 on vikunja.example.com');
	expect(history.entries()).toEqual(['/task/vikunja.example.com/102']);
});

test('an unknown path says so', async () => {
	await renderAt('/tasks/102');
	await expect.element(page.getByRole('heading', { level: 1 })).toHaveTextContent('Not found');
	await expect.element(page.getByText('/tasks/102')).toBeVisible();
});

test('the configuration is loaded once, and links the runs to their Kelos console', async () => {
	const { server } = await renderAt('/?q=widgets', {
		[CONFIG]: () => json(goldenResponse(CONFIG)),
		[`${API_BASE}/search`]: () =>
			json({
				query: 'widgets',
				roots: [{ root: { task: { instance: 'vikunja.example.com', id: 102 } }, title: 'Widgets' }]
			}),
		[TASK_STORY]: () => json(goldenStory('implementation-merged'))
	});
	await page.getByRole('link', { name: 'Widgets' }).click();
	await page
		.getByRole('region', { name: 'Events' })
		.getByRole('button', { name: /Run vikunja-widgets-agent-opus-102/ })
		.click();
	await expect
		.element(
			page.getByRole('region', { name: 'Detail' }).getByRole('link', { name: /Kelos console/ })
		)
		.toHaveAttribute(
			'href',
			'https://kelos.example.com/api/resources/tasks/agents/vikunja-widgets-agent-opus-102/logs'
		);
	expect(server.requests.filter((r) => r.path === CONFIG)).toHaveLength(1);
});

test('without the configuration, the story still shows, its runs without a link', async () => {
	await renderAt('/task/vikunja.example.com/102', {
		[TASK_STORY]: () => json(goldenStory('implementation-merged'))
	});
	await page
		.getByRole('region', { name: 'Events' })
		.getByRole('button', { name: /Run vikunja-widgets-agent-opus-102/ })
		.click();
	const detail = page.getByRole('region', { name: 'Detail' });
	await expect.element(detail.getByRole('heading', { level: 3 })).toHaveTextContent(/^Run /);
	expect(detail.getByRole('link', { name: /Kelos console/ }).elements()).toHaveLength(0);
});

test('the story page takes the whole width, the entry page a narrow column', async () => {
	await page.viewport(WIDE.width, WIDE.height);
	const { router } = await renderAt('/', {
		[TASK_STORY]: () => json(goldenStory('implementation-merged'))
	});
	const main = () => page.getByRole('main').element().getBoundingClientRect().width;
	await expect.element(page.getByRole('search')).toBeVisible();
	expect(main()).toBeLessThanOrEqual(NARROW_PX);
	router.navigate('/task/vikunja.example.com/102');
	await expect.element(page.getByRole('region', { name: 'Timeline' })).toBeVisible();
	expect(main()).toBeGreaterThan(NARROW_PX);
});
