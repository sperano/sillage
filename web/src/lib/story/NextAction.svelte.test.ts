import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import NextAction from './NextAction.svelte';
import { goldenStory } from '../testing/stories';

const region = () => page.getByRole('region', { name: 'Next action' });

test('unknown without explicit evidence, however long the story is quiet', async () => {
	render(NextAction, { story: goldenStory('announced-run-only'), timeZone: 'UTC' });
	await expect.element(region()).toHaveTextContent('Unknown: no pending review request');
});

test('a pending review request names the reviewer', async () => {
	const story = goldenStory('implementation-merged');
	story.events = story.events.filter(
		(e) => e.kind !== 'pr.merged' && e.kind !== 'pr.review_submitted'
	);
	const opened = story.events.find((e) => e.kind === 'pr.opened');
	if (!opened) throw new Error('no pr.opened');
	story.events.push({
		...opened,
		id: 'issues/events/7002',
		kind: 'pr.review_requested',
		sourceTime: '2026-03-03T18:40:00Z',
		observedAt: '2026-03-03T18:41:00Z',
		executor: undefined,
		details: { reviewer: 'reviewer-a' }
	});
	render(NextAction, { story, timeZone: 'UTC' });
	await expect
		.element(region())
		.toHaveTextContent(
			'Review by reviewer-a on example-org/widgets#7, requested 2026-03-03 18:40:00 UTC'
		);
});

test('a request with no announcement is waiting for an agent', async () => {
	const story = goldenStory('failed-run-retry');
	const announced = story.events.find((e) => e.kind === 'run.announced')!;
	story.events = story.events.filter((e) => e.kind !== 'run.announced');
	story.events.push({
		...announced,
		id: 'comments/5499',
		kind: 'request.created',
		sourceTime: '2026-03-05T14:30:00Z',
		actor: { login: 'owner', kind: 'person' },
		details: { command: '/gordon' }
	});
	render(NextAction, { story, timeZone: 'UTC' });
	await expect
		.element(region())
		.toHaveTextContent(
			'Waiting for an agent: /gordon by owner 2026-03-05 14:30:00 UTC, no start announced'
		);
});

test('a request seen by polling is not given the poll as its time', async () => {
	const story = goldenStory('failed-run-retry');
	const announced = story.events.find((e) => e.kind === 'run.announced')!;
	story.events = story.events.filter((e) => e.kind !== 'run.announced');
	story.events.push({
		...announced,
		id: 'comments/5499',
		kind: 'request.created',
		sourceTime: undefined,
		observedAt: '2026-03-05T14:32:00Z',
		actor: { login: 'owner', kind: 'person' },
		details: { command: '/gordon' }
	});
	render(NextAction, { story, timeZone: 'UTC' });
	await expect
		.element(region())
		.toHaveTextContent('by owner (seen 2026-03-05 14:32:00 UTC, no source time)');
});
