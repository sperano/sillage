import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import CoverageNotices from './CoverageNotices.svelte';
import { goldenStory } from '../testing/stories';

test('each source: its last successful read, unknown after it, and its gaps with reasons', async () => {
	render(CoverageNotices, { story: goldenStory('announced-run-only'), timeZone: 'UTC' });
	const kelos = page.getByRole('listitem').filter({ hasText: 'Kelos cluster-a' }).first();
	await expect
		.element(kelos)
		.toHaveTextContent(
			'last successful read 2026-03-01 12:00:00 UTC; unknown after 2026-03-01 12:00:00 UTC.'
		);
	await expect
		.element(kelos.getByRole('listitem'))
		.toHaveTextContent(
			"Unknown from 2026-01-01 00:00:00 UTC to 2026-02-27 00:00:00 UTC: before the collector's first read; Tasks and TaskRecords of this period had expired"
		);
	await expect
		.element(page.getByRole('listitem').filter({ hasText: 'Vikunja vikunja.example.com' }))
		.toHaveTextContent('unknown after 2026-03-01 12:00:00 UTC');
});

test('a source never read, a note, and a source with no coverage record', async () => {
	const story = goldenStory('implementation-merged');
	story.coverage = [
		{ source: 'github', instance: 'github.example.com', notes: ['checks of 3 commits not read'] },
		{
			source: 'vikunja',
			instance: 'vikunja.example.com',
			lastSuccessfulRead: '2026-03-03T20:00:00Z'
		}
	];
	render(CoverageNotices, { story, timeZone: 'UTC' });
	const github = page
		.getByRole('listitem')
		.filter({ hasText: 'GitHub github.example.com' })
		.first();
	await expect.element(github).toHaveTextContent('never read successfully');
	await expect.element(github).toHaveTextContent('checks of 3 commits not read');
	await expect
		.element(page.getByRole('listitem').filter({ hasText: 'Kelos cluster-a' }))
		.toHaveTextContent('no coverage record; how far it was read is unknown.');
});
