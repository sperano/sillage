import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import App from './App.svelte';
import { API_BASE } from './lib/api';
import { memoryHistory } from './lib/history';
import { Router } from './lib/router.svelte';
import { HANG, fakeServer, json } from './testing/fakeServer';
import { goldenStory } from './testing/goldenStories';

const TASK_STORY = `${API_BASE}/stories/task/vikunja.example.com/102`;
const PAGE_STORY = `${API_BASE}/stories/page/wiki.example.com/57/en`;

async function renderAt(href: string, handlers: Parameters<typeof fakeServer>[0] = {}) {
	const history = memoryHistory(href);
	const server = fakeServer({ [`${API_BASE}/coverage`]: () => json([]), ...handlers });
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

test('a deep link to a task or a page loads its story', async () => {
	await renderAt('/page/wiki.example.com/57/en', {
		[PAGE_STORY]: () => json(goldenStory('wiki-rooted-request'))
	});
	await expect
		.element(page.getByRole('heading', { level: 1 }))
		.toHaveTextContent('History of page 57 (en) on wiki.example.com');
	await expect.element(page.getByText('6 events, 2 runs')).toBeVisible();
});

test('a search result opens its story in the page, and back returns to the results', async () => {
	const { history, server } = await renderAt('/?q=widgets', {
		[`${API_BASE}/search`]: () =>
			json([{ root: { task: { instance: 'vikunja.example.com', id: 102 } }, title: 'Widgets' }]),
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
		[`${API_BASE}/resolve`]: (url) =>
			url.searchParams.get('url') === link
				? json({ task: { instance: 'vikunja.example.com', id: 102 } })
				: json({}, 400),
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
