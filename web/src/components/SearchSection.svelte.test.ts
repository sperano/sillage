import { expect, test } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { API_BASE, MAX_SEARCH_LENGTH } from '../lib/api';
import { memoryHistory } from '../lib/history';
import { Router } from '../lib/router.svelte';
import { HANG, type Handler, fakeServer, json, problem } from '../testing/fakeServer';
import SearchSection from './SearchSection.svelte';

const SEARCH = `${API_BASE}/search`;

// matches answers a search with roots, as the server does: under the query it read.
function matches(roots: unknown[] = []): Handler {
	return (url) => json({ query: url.searchParams.get('q'), roots });
}

async function renderSearch(query: string, handlers: Parameters<typeof fakeServer>[0] = {}) {
	const history = memoryHistory('/');
	const router = new Router(history);
	const server = fakeServer(handlers);
	const view = await render(SearchSection, { api: server.api, router, query });
	return { history, server, view };
}

test('submitting puts the search in the URL', async () => {
	const { history, server } = await renderSearch('');
	await userEvent.type(page.getByRole('searchbox', { name: 'Title or key' }), '  widgets {Enter}');
	expect(history.entries()).toEqual(['/', '/?q=widgets']);
	expect(server.requests).toEqual([]);
});

test('lists the matching roots with links to their stories', async () => {
	const { server } = await renderSearch('widgets', {
		[SEARCH]: matches([
			{
				root: { task: { instance: 'vikunja.example.com', id: 102 } },
				title: 'Widgets: add a gear',
				lastActivity: '2026-03-03T19:30:00Z'
			},
			{ root: { task: { instance: 'vikunja.example.com', id: 103 } } }
		])
	});
	const results = page.getByRole('list', { name: 'Search results' });
	await expect
		.element(results.getByRole('link', { name: 'Widgets: add a gear' }))
		.toHaveAttribute('href', '/task/vikunja.example.com/102');
	await expect
		.element(results.getByRole('listitem').first())
		.toHaveTextContent('task 102 on vikunja.example.com last activity 2026-03-03 19:30:00 UTC');
	await expect
		.element(results.getByRole('link', { name: 'task 103 on vikunja.example.com' }))
		.toHaveAttribute('href', '/task/vikunja.example.com/103');
	expect(server.requests.map((r) => r.path)).toEqual([`${SEARCH}?q=widgets`]);
	await expect.element(page.getByRole('searchbox')).toHaveValue('widgets');
});

test('a search from a link longer than the box takes is not sent', async () => {
	const query = 'w'.repeat(MAX_SEARCH_LENGTH + 1);
	const { server } = await renderSearch(query, { [SEARCH]: matches() });
	await expect
		.element(page.getByRole('alert'))
		.toHaveTextContent(
			`A search is at most ${MAX_SEARCH_LENGTH} characters long; this one has ${query.length}.`
		);
	expect(server.requests).toEqual([]);
});

test('no match says so', async () => {
	await renderSearch('nothing', { [SEARCH]: matches() });
	await expect.element(page.getByRole('status')).toHaveTextContent('No task matches “nothing”.');
});

test('a failed search says why', async () => {
	await renderSearch('widgets', {
		[SEARCH]: () => problem(400, 'Bad Request', 'the search is too long')
	});
	await expect
		.element(page.getByRole('alert'))
		.toHaveTextContent(
			"Sillage's server answered with an error (400 Bad Request): the search is too long."
		);
});

test('searching again for the same text retries, in place', async () => {
	let failures = 1;
	const { history, server } = await renderSearch('widgets', {
		[SEARCH]: (url) => (failures-- > 0 ? problem(503, 'Service Unavailable') : matches()(url))
	});
	await expect.element(page.getByRole('alert')).toBeVisible();
	await page.getByRole('button', { name: 'Search' }).click();
	await expect.element(page.getByRole('status')).toHaveTextContent('No task matches');
	expect(server.requests).toHaveLength(2);
	expect(history.entries()).toEqual(['/']);
});

test('a new search aborts the one in flight', async () => {
	const { server, view } = await renderSearch('first', {
		[SEARCH]: (url) => (url.searchParams.get('q') === 'first' ? HANG : matches()(url))
	});
	await expect.element(page.getByRole('status')).toHaveTextContent('Searching');
	await view.rerender({ query: 'second' });
	await expect.element(page.getByRole('status')).toHaveTextContent('No task matches “second”.');
	expect(server.requests.map((r) => r.signal.aborted)).toEqual([true, false]);
});
