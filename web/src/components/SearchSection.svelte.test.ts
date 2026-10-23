import { expect, test } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { API_BASE } from '../lib/api';
import { memoryHistory } from '../lib/history';
import { Router } from '../lib/router.svelte';
import { HANG, fakeServer, json, problem } from '../testing/fakeServer';
import SearchSection from './SearchSection.svelte';

const SEARCH = `${API_BASE}/search`;

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
		[SEARCH]: () =>
			json([
				{
					root: { task: { instance: 'vikunja.example.com', id: 102 } },
					title: 'Widgets: add a gear',
					lastActivity: '2026-03-03T19:30:00Z'
				},
				{ root: { page: { instance: 'wiki.example.com', id: 57, locale: 'en' } } }
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
		.element(results.getByRole('link', { name: 'page 57 (en) on wiki.example.com' }))
		.toHaveAttribute('href', '/page/wiki.example.com/57/en');
	expect(server.requests.map((r) => r.path)).toEqual([`${SEARCH}?q=widgets`]);
	await expect.element(page.getByRole('searchbox')).toHaveValue('widgets');
});

test('no match says so', async () => {
	await renderSearch('nothing', { [SEARCH]: () => json([]) });
	await expect
		.element(page.getByRole('status'))
		.toHaveTextContent('No task or page matches “nothing”.');
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

test('a new search aborts the one in flight', async () => {
	const { server, view } = await renderSearch('first', {
		[SEARCH]: (url) => (url.searchParams.get('q') === 'first' ? HANG : json([]))
	});
	await expect.element(page.getByRole('status')).toHaveTextContent('Searching');
	await view.rerender({ query: 'second' });
	await expect
		.element(page.getByRole('status'))
		.toHaveTextContent('No task or page matches “second”.');
	expect(server.requests.map((r) => r.signal.aborted)).toEqual([true, false]);
});
