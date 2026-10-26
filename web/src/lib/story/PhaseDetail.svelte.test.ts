import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import PhaseDetail from './PhaseDetail.svelte';
import { Selection } from '../selection.svelte';
import { goldenStory } from '../testing/stories';
import type { PhaseState } from '../model';
import { runRef } from '../timeline';

const STORY = 'phase-markers';

/** Shows the first phase in state of the story's runs, with the selection it selects through. */
function show(state: PhaseState) {
	const story = goldenStory(STORY);
	const run = story.runs.find((r) => r.phases?.some((p) => p.state === state));
	const phase = run?.phases?.find((p) => p.state === state);
	if (!run || !phase) throw new Error(`no ${state} phase`);
	const selection = new Selection();
	render(PhaseDetail, { run, phase, selection, timeZone: 'UTC' });
	return { run, phase, selection };
}

test('a complete phase has its outcome, both edges and a duration', async () => {
	show('complete');
	await expect
		.element(page.getByRole('heading', { level: 3 }))
		.toHaveTextContent('blocker wait: unblocked');
	await expect.element(page.getByTestId('phase-start')).toHaveTextContent('2026-03-14');
	await expect.element(page.getByText('3 min 30 s')).toBeVisible();
});

test('a phase joined by Task name and time says it is uncertain', async () => {
	show('complete');
	await expect.element(page.getByRole('heading', { level: 3 })).toHaveTextContent('(uncertain)');
	await expect
		.element(page.getByTestId('phase-join'))
		.toHaveTextContent(/^uncertain: by the Task name/);
});

test('a phase in progress has an open end, an interrupted one no end', async () => {
	show('in-progress');
	await expect
		.element(page.getByTestId('phase-end'))
		.toHaveTextContent('open: not ended at the last read');
});

test('an interrupted phase is never closed at the run’s end', async () => {
	show('interrupted');
	await expect.element(page.getByTestId('phase-end')).toHaveTextContent('not recorded');
	await expect
		.element(page.getByTestId('phase-state'))
		.toHaveTextContent('never closed at the run’s end');
});

test('the run link selects the run', async () => {
	const { run, selection } = show('start-not-recorded');
	await page.getByRole('button', { name: run.taskName }).click();
	expect(selection.current).toEqual(runRef(run));
});
