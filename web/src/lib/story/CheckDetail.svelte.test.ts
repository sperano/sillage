import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import CheckDetail from './CheckDetail.svelte';
import { goldenStory } from '../testing/stories';

test('the conclusion is shown in words', async () => {
	const pr = goldenStory('implementation-merged').pullRequests[0];
	const check = pr?.checks[0];
	if (!pr || !check) throw new Error('no check');
	check.conclusion = 'timed_out';
	render(CheckDetail, { pr, check, timeZone: 'UTC' });
	await expect.element(page.getByRole('heading')).toHaveTextContent(/: timed out$/);
	await expect.element(page.getByText('timed out', { exact: true })).toBeInTheDocument();
});
