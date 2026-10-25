import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { API_BASE } from '../lib/api';
import { memoryHistory } from '../lib/history';
import { Router } from '../lib/router.svelte';
import { openHref } from '../lib/routes';
import { HANG, fakeServer, json, problem } from '../testing/fakeServer';
import OpenPage from './OpenPage.svelte';

const RESOLVE = `${API_BASE}/resolve`;
const LINK = 'https://vikunja.example.com/tasks/102';

async function renderOpen(url: string, handlers: Parameters<typeof fakeServer>[0]) {
	const history = memoryHistory(openHref(url));
	const router = new Router(history);
	const server = fakeServer(handlers);
	await render(OpenPage, { api: server.api, router, url });
	return { history, server };
}

test('replaces /open with the story of the resolved root', async () => {
	const { history, server } = await renderOpen(LINK, {
		[RESOLVE]: () =>
			json({
				kind: 'vikunja-task',
				roots: [{ root: { page: { instance: 'wiki.example.com', id: 57, locale: 'en' } } }]
			})
	});
	await expect.poll(() => history.entries()).toEqual(['/page/wiki.example.com/57/en']);
	expect(server.requests.map((r) => r.path)).toEqual([
		`${RESOLVE}?url=${encodeURIComponent(LINK)}`
	]);
});

test('a link several roots hold lists them, most recently active first, and stays', async () => {
	const pull = 'https://github.example.com/example-org/widgets/pull/7';
	const { history } = await renderOpen(pull, {
		[RESOLVE]: () =>
			json({
				kind: 'github-pull-request',
				roots: [
					{ root: { task: { instance: 'vikunja.example.com', id: 103 } }, title: 'Gadgets' },
					{ root: { task: { instance: 'vikunja.example.com', id: 102 } }, title: 'Widgets' }
				]
			})
	});
	const roots = page.getByRole('list', { name: 'Tasks and pages of the link' });
	await expect
		.element(roots.getByRole('listitem').first())
		.toHaveTextContent('Gadgets task 103 on vikunja.example.com');
	await expect
		.element(roots.getByRole('link', { name: 'Widgets' }))
		.toHaveAttribute('href', '/task/vikunja.example.com/102');
	expect(history.entries()).toEqual([openHref(pull)]);
});

test('says while it looks the link up', async () => {
	await renderOpen(LINK, { [RESOLVE]: () => HANG });
	await expect.element(page.getByRole('status')).toHaveTextContent(`Looking up ${LINK}`);
});

test('a link sillage cannot resolve says so and stays on /open', async () => {
	const { history } = await renderOpen(LINK, { [RESOLVE]: () => problem(404, 'Not Found') });
	await expect
		.element(page.getByRole('alert'))
		.toHaveTextContent('Sillage knows no task or page for this link');
	await expect.element(page.getByText(LINK)).toBeVisible();
	expect(history.entries()).toEqual([openHref(LINK)]);
});

test('without a link it asks for none', async () => {
	const { server } = await renderOpen('', {});
	await expect.element(page.getByText('There is no link to open.')).toBeVisible();
	expect(server.requests).toEqual([]);
});
