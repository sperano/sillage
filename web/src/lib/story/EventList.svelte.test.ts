import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { cleanup, render } from 'vitest-browser-svelte';
import EventList from './EventList.svelte';
import { Selection } from '../selection.svelte';
import { goldenStory } from '../testing/stories';
import { timelineItems } from '../timeline';
import type { Story } from '../model';
import type { LinkConfig } from '../links';

function show(story: Story, links: LinkConfig = {}) {
	render(EventList, {
		story,
		items: timelineItems(story),
		selection: new Selection(),
		links,
		timeZone: 'UTC'
	});
}

const row = (text: string | RegExp) => page.getByRole('listitem').filter({ hasText: text });

test('a row shows the account, the executor and an uncertain attribution', async () => {
	show(goldenStory('implementation-merged'));
	const opened = row('Pull request opened');
	await expect.element(opened).toHaveTextContent('owner-gh (person)');
	await expect.element(opened).toHaveTextContent('executor agent-opus (agent)');
	await expect.element(opened).not.toHaveTextContent('attribution uncertain');
	await expect.element(row('Pull request merged')).toHaveTextContent('attribution uncertain');
	await expect.element(row('Task created')).not.toHaveTextContent('attribution uncertain');
});

test('a row shows the observed time only when it differs from the source time', async () => {
	show(goldenStory('implementation-merged'));
	await expect
		.element(row('Pull request merged'))
		.toHaveTextContent('2026-03-03 19:12:00 UTC · observed 2026-03-03 19:13:00 UTC');
	const polled = row('Seen in bucket Review');
	await expect.element(polled).toHaveTextContent('seen 2026-03-03 18:26:00 UTC (no source time)');
	await expect.element(polled).toHaveTextContent('unknown (seen by polling)');

	const same = goldenStory('implementation-merged');
	for (const e of same.events) {
		e.observedAt = e.sourceTime ?? e.observedAt;
	}
	cleanup();
	show(same);
	await expect.element(row('Pull request merged')).not.toHaveTextContent('observed');
});

test('each row links its source record; runs need a configured console', async () => {
	const story = goldenStory('failed-run-retry');
	show(story);
	await expect
		.element(row('Run announced').first().getByRole('link'))
		.toHaveAttribute('href', 'https://vikunja.example.com/tasks/104#comment-5401');
	await expect
		.element(row('Run vikunja-widgets-agent-opus-104 (implementation): failed').getByRole('link'))
		.not.toBeInTheDocument();

	cleanup();
	show(story, { kelosConsoles: { 'cluster-a': 'https://kelos.example.com' } });
	await expect
		.element(row('Run vikunja-widgets-agent-opus-104 (implementation): failed').getByRole('link'))
		.toHaveAttribute(
			'href',
			'https://kelos.example.com/api/resources/tasks/agents/vikunja-widgets-agent-opus-104/logs'
		);
});

test('checks are rows, each linking its run', async () => {
	show(goldenStory('implementation-merged'));
	const failure = row(/Check test on aaaaaaa .*failed/);
	await expect.element(failure).toHaveTextContent('started 2026-03-03 18:24:10 UTC');
	await expect
		.element(failure.getByRole('link'))
		.toHaveAttribute('href', 'https://github.example.com/example-org/widgets/runs/9001');
});

test('row links are not tab stops: the list is one', async () => {
	show(goldenStory('implementation-merged'));
	const links = page.getByRole('link').elements();
	expect(links.length).toBeGreaterThan(0);
	expect(links.every((l) => l.tabIndex === -1)).toBe(true);
});
