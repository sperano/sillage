import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import RunDetail from './RunDetail.svelte';
import { Selection } from '../selection.svelte';
import { goldenStory } from '../testing/stories';
import { timelineItems } from '../timeline';
import type { Usage } from '../model';

function showUsage(usage: Usage) {
	const story = goldenStory('failed-run-retry');
	const run = story.runs[0];
	if (!run) throw new Error('no run');
	run.usage = usage;
	render(RunDetail, {
		story,
		items: timelineItems(story),
		selection: new Selection(),
		run,
		links: {},
		timeZone: 'UTC'
	});
}

test('usage without a cost starts with no separator', async () => {
	showUsage({ inputTokens: 1200, outputTokens: 300 });
	await expect
		.element(page.getByTestId('usage'))
		.toHaveTextContent(/^1200 input tokens · 300 output tokens$/);
});

test('usage with a cost separates every value', async () => {
	showUsage({ costUSD: '0.42', outputTokens: 300 });
	await expect
		.element(page.getByTestId('usage'))
		.toHaveTextContent(/^US\$0\.42 · 300 output tokens$/);
});

test('a run lists its phases, each selecting it', async () => {
	const story = goldenStory('phase-markers');
	const run = story.runs[1]!;
	const selection = new Selection();
	render(RunDetail, {
		story,
		items: timelineItems(story),
		selection,
		run,
		links: {},
		timeZone: 'UTC'
	});
	await page.getByRole('button', { name: 'agent: in progress' }).click();
	expect(selection.current?.kind).toBe('phase');
});

test('a run without phases lists none', async () => {
	const story = goldenStory('failed-run-retry');
	render(RunDetail, {
		story,
		items: timelineItems(story),
		selection: new Selection(),
		run: story.runs[0]!,
		links: {},
		timeZone: 'UTC'
	});
	await expect.element(page.getByRole('heading', { level: 3 })).toBeVisible();
	expect(document.querySelector('[data-testid="phases"]')).toBeNull();
});
