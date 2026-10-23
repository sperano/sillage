import { describe, expect, test } from 'vitest';
import { goldenStory } from './testing/stories';
import { checkURL, eventLink, pageURL, runLink } from './links';
import type { Story } from './model';

function linkOf(story: Story, id: string, config = {}) {
	const e = story.events.find((x) => x.id === id);
	if (!e) throw new Error(`no event ${id}`);
	return eventLink(story, e, config);
}

describe('wiki-rooted-request', () => {
	const story = goldenStory('wiki-rooted-request');
	const page = 'https://wiki.example.com/en/widgets/design/storage-layout';

	test('the live edit links the page as it is now', () => {
		const live = story.events.find((e) => e.details?.version === 'live');
		expect(live && eventLink(story, live, {})).toEqual({
			href: page,
			label: 'wiki page, live version'
		});
	});

	test('a version links its source by version id', () => {
		expect(linkOf(story, 'pages/57/versions/121')?.href).toBe(
			'https://wiki.example.com/s/en/widgets/design/storage-layout?v=121'
		);
	});

	test('a comment links its anchor on the page', () => {
		expect(linkOf(story, 'comments/880')?.href).toBe(`${page}#comment-post-id-880`);
	});

	test('a move changes the path every link uses', () => {
		const moved = structuredClone(story);
		const created = moved.events[0];
		if (!created) throw new Error('no events');
		moved.events.push({
			...created,
			id: 'pages/57/moves/1',
			kind: 'page.moved',
			details: { from: 'widgets/design/storage-layout', to: 'widgets/storage' }
		});
		expect(linkOf(moved, 'comments/882')?.href).toBe(
			'https://wiki.example.com/en/widgets/storage#comment-post-id-882'
		);
	});

	test('a page no event names the path of is linked by id', () => {
		const pathless = structuredClone(story);
		pathless.events = pathless.events.filter((e) => e.kind !== 'page.created');
		expect(pageURL(pathless, { instance: 'wiki.example.com', id: 57, locale: 'en' })).toBe(
			'https://wiki.example.com/i/57'
		);
		expect(linkOf(pathless, 'pages/57/versions/121')?.href).toBe('https://wiki.example.com/i/57');
	});
});

describe('implementation-merged', () => {
	const story = goldenStory('implementation-merged');
	const pr = 'https://github.example.com/example-org/widgets/pull/7';

	test.each([
		['pulls/7', pr],
		['reviews/3001', `${pr}#pullrequestreview-3001`],
		['issues/events/7001', `${pr}#event-7001`],
		['comments/5102', 'https://vikunja.example.com/tasks/102#comment-5102'],
		['tasks/102', 'https://vikunja.example.com/tasks/102']
	])('%s links %s', (id, href) => {
		expect(linkOf(story, id)?.href).toBe(href);
	});

	test('a polling observation links its task', () => {
		const done = story.events.find((e) => e.kind === 'task.done');
		expect(done && eventLink(story, done, {})?.href).toBe('https://vikunja.example.com/tasks/102');
	});

	test('a check links its run on GitHub', () => {
		const p = story.pullRequests[0];
		const c = p?.checks[0];
		expect(p && c && checkURL(p, c)).toBe(
			`https://github.example.com/example-org/widgets/runs/${c?.id}`
		);
	});
});

describe('Kelos console', () => {
	const story = goldenStory('failed-run-retry');
	const run = story.runs[0];
	if (!run) throw new Error('no run');

	test('links the Task logs when the cluster has a console', () => {
		expect(runLink(run, { kelosConsoles: { 'cluster-a': 'https://kelos.example.com' } })).toEqual({
			href: 'https://kelos.example.com/api/resources/tasks/agents/vikunja-widgets-agent-opus-104/logs',
			label: 'Kelos console: Task vikunja-widgets-agent-opus-104'
		});
	});

	test('links nothing without a configured console', () => {
		expect(runLink(run, {})).toBeUndefined();
		expect(
			runLink(run, { kelosConsoles: { 'cluster-b': 'https://kelos.example.com' } })
		).toBeUndefined();
	});
});
