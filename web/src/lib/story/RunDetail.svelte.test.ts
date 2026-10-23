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
