import { describe, expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { parseStory } from '../lib/decodeModel';
import type { Coverage } from '../lib/model';
import { goldenStories, goldenStory } from '../testing/goldenStories';
import Freshness from './Freshness.svelte';

function coverageOf(name: string): Coverage[] {
	return parseStory(goldenStory(name)).coverage;
}

describe('golden stories', () => {
	test.each([...goldenStories.keys()])("%s: each source's last read", async (name) => {
		const coverage = coverageOf(name);
		await render(Freshness, { coverage });
		const items = page.getByRole('listitem').filter({ has: page.getByRole('heading') });
		await expect.element(items.first()).toBeVisible();
		expect(items.elements()).toHaveLength(coverage.length);
		for (const c of coverage) {
			await expect.element(page.getByRole('heading', { name: c.instance })).toBeVisible();
		}
	});

	test('a gap reads as missing records with its reason', async () => {
		await render(Freshness, { coverage: coverageOf('announced-run-only') });
		const kelos = page.getByRole('listitem').filter({ hasText: 'cluster-a' });
		await expect
			.element(kelos)
			.toHaveTextContent('Last read successfully at 2026-03-01 12:00:00 UTC.');
		await expect
			.element(kelos)
			.toHaveTextContent('Records are missing, so what happened is unknown');
		await expect
			.element(kelos.getByRole('listitem'))
			.toHaveTextContent(
				'from 2026-01-01 00:00:00 UTC to 2026-02-27 00:00:00 UTC: Kelos deletes a finished Task after the configured TTL: a run before then is read only from its TaskRecord, which only a run that reported usage leaves'
			);
	});
});

test('a source never read, and notes, are said plainly', async () => {
	await render(Freshness, {
		coverage: [
			{ source: 'wiki', instance: 'wiki.example.com' },
			{
				source: 'github',
				instance: 'github.example.com',
				lastSuccessfulRead: '2026-03-03T20:00:00Z',
				notes: ['commit bbbb: checks not read']
			}
		]
	});
	await expect
		.element(page.getByRole('listitem').filter({ hasText: 'wiki.example.com' }))
		.toHaveTextContent('Never read successfully: nothing from it is known.');
	await expect
		.element(page.getByRole('heading', { name: 'Wiki.js wiki.example.com' }))
		.toBeVisible();
	await expect.element(page.getByText('commit bbbb: checks not read')).toBeVisible();
	await expect
		.element(page.getByText('2026-03-03 20:00:00 UTC'))
		.toHaveAttribute('datetime', '2026-03-03T20:00:00Z');
});

test('no coverage at all says nothing is known', async () => {
	await render(Freshness, { coverage: [] });
	await expect
		.element(page.getByText('No source has been read yet: nothing is known.'))
		.toBeVisible();
});
