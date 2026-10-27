import { describe, expect, test } from 'vitest';
import { goldenStory } from './testing/stories';
import { checkURL, eventLink, runLink } from './links';
import type { Story } from './model';

function linkOf(story: Story, id: string, config = {}) {
	const e = story.events.find((x) => x.id === id);
	if (!e) throw new Error(`no event ${id}`);
	return eventLink(story, e, config);
}

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

	test('a console under a path prefix keeps it', () => {
		expect(
			runLink(run, { kelosConsoles: { 'cluster-a': 'https://ops.example.com/kelos/' } })?.href
		).toBe(
			'https://ops.example.com/kelos/api/resources/tasks/agents/vikunja-widgets-agent-opus-104/logs'
		);
		expect(runLink(run, { kelosConsoles: { 'cluster-a': 'not a url' } })).toBeUndefined();
	});

	test.each(['javascript:alert(1)//', 'data:text/html,x', 'ftp://kelos.example.com'])(
		'a console at %s links nothing',
		(base) => {
			expect(runLink(run, { kelosConsoles: { 'cluster-a': base } })).toBeUndefined();
		}
	);

	test('a console over http keeps its scheme and port', () => {
		expect(
			runLink(run, { kelosConsoles: { 'cluster-a': 'http://kelos.example.com:8080' } })?.href
		).toBe(
			'http://kelos.example.com:8080/api/resources/tasks/agents/vikunja-widgets-agent-opus-104/logs'
		);
	});

	test('links nothing without a configured console', () => {
		expect(runLink(run, {})).toBeUndefined();
		expect(
			runLink(run, { kelosConsoles: { 'cluster-b': 'https://kelos.example.com' } })
		).toBeUndefined();
	});
});
