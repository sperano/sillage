import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import RootList from './RootList.svelte';

test('each root links to its story, by its title or else its key', async () => {
	await render(RootList, {
		label: 'Matches',
		roots: [
			{
				root: { task: { instance: 'vikunja.example.com', id: 102 } },
				title: 'Widgets: add a gear',
				lastActivity: '2026-03-03T19:30:00Z'
			},
			{ root: { page: { instance: 'wiki.example.com', id: 57, locale: 'en' } } }
		]
	});
	const list = page.getByRole('list', { name: 'Matches' });
	await expect
		.element(list.getByRole('listitem').first())
		.toHaveTextContent(
			'Widgets: add a gear task 102 on vikunja.example.com last activity 2026-03-03 19:30:00 UTC'
		);
	await expect
		.element(list.getByRole('link', { name: 'page 57 (en) on wiki.example.com' }))
		.toHaveAttribute('href', '/page/wiki.example.com/57/en');
	await expect
		.element(list.getByText('2026-03-03 19:30:00 UTC'))
		.toHaveAttribute('datetime', '2026-03-03T19:30:00Z');
});
