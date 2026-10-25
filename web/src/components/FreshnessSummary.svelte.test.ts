import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import FreshnessSummary from './FreshnessSummary.svelte';

test('one read root has one read, said once', async () => {
	await render(FreshnessSummary, {
		sources: [
			{
				source: 'vikunja',
				instance: 'vikunja.example.com',
				roots: 1,
				neverRead: 0,
				newestRead: '2026-03-03T20:00:00Z',
				oldestRead: '2026-03-03T20:00:00Z'
			}
		]
	});
	await expect
		.element(page.getByRole('listitem'))
		.toHaveTextContent(
			'Vikunja vikunja.example.com Read for 1 task or page. Last read successfully at 2026-03-03 20:00:00 UTC.'
		);
	await expect
		.element(page.getByText('2026-03-03 20:00:00 UTC'))
		.toHaveAttribute('datetime', '2026-03-03T20:00:00Z');
});

test('roots never read are counted, and a source never read is said plainly', async () => {
	await render(FreshnessSummary, {
		sources: [
			{
				source: 'github',
				instance: 'github.example.com',
				roots: 3,
				neverRead: 2,
				newestRead: '2026-03-03T20:00:00Z',
				oldestRead: '2026-03-03T20:00:00Z'
			},
			{ source: 'wiki', instance: 'wiki.example.com', roots: 2, neverRead: 2 }
		]
	});
	await expect
		.element(page.getByRole('listitem').filter({ hasText: 'github.example.com' }))
		.toHaveTextContent(
			'2 tasks or pages of them never read successfully: nothing from it is known for those.'
		);
	const wiki = page.getByRole('listitem').filter({ hasText: 'wiki.example.com' });
	await expect
		.element(wiki)
		.toHaveTextContent('Never read successfully: nothing from it is known.');
	await expect.element(wiki).not.toHaveTextContent('Last read successfully');
});

test('no source at all says nothing is known', async () => {
	await render(FreshnessSummary, { sources: [] });
	await expect
		.element(page.getByText('No source has been read yet: nothing is known.'))
		.toBeVisible();
});
