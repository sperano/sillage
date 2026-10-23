import { expect, test } from 'vitest';
import { goldenStory } from './testing/stories';
import { conclusionLabel, eventLabel, refLabel, runLabel } from './describe';
import type { CheckConclusion, Event, EventKind } from './model';

const story = goldenStory('wiki-rooted-request');

function event(id: string): Event {
	const e = story.events.find((x) => x.id === id);
	if (!e) throw new Error(`no event ${id}`);
	return e;
}

test('events are named with their details', () => {
	expect(event('pages/57/versions/121').kind).toBe('page.edited');
	expect(eventLabel(event('pages/57/versions/121'))).toBe('Page edited');
	expect(eventLabel({ ...event('pages/57/versions/121'), details: { version: '121' } })).toBe(
		'Page edited (version 121)'
	);
	expect(eventLabel(event('comments/880'))).toBe('Request /kelos');
	expect(
		eventLabel({ ...event('comments/880'), details: { command: '/gordon', agent: 'agent-sol' } })
	).toBe('Request /gordon for agent-sol');
	expect(eventLabel(event('comments/881'))).toBe('Run announced: wiki-fu-widgets-57-c880');
});

test('a kind newer than this build is shown as it is named', () => {
	expect(eventLabel({ ...event('comments/882'), kind: 'run.observed' as EventKind })).toBe(
		'run.observed'
	);
});

test('a run is named by its Task, its role and its outcome', () => {
	const run = goldenStory('announced-run-only').runs[0];
	expect(run && runLabel(run)).toBe(
		'Run vikunja-widgets-agent-opus-105 (implementation): outcome unknown'
	);
});

test('a correlation end is named by the record it points to', () => {
	expect(refLabel(story, { kind: 'page', id: 'wiki.example.com/pages/57/en' })).toBe(
		'Page 57 (this story)'
	);
	const discuss = goldenStory('discuss-no-pr');
	expect(refLabel(discuss, { kind: 'page', id: 'wiki.example.com/pages/41/en' })).toBe(
		'Wiki page widgets/reports/caching-options'
	);
	expect(refLabel(story, { kind: 'run', id: 'cluster-b/agents/uid/x' })).toBe(
		'run cluster-b/agents/uid/x'
	);
});

test('check conclusions are named in words, an unknown one as it is', () => {
	expect(conclusionLabel('timed_out')).toBe('timed out');
	expect(conclusionLabel('action_required')).toBe('action required');
	expect(conclusionLabel('success')).toBe('succeeded');
	expect(conclusionLabel('later_value' as CheckConclusion)).toBe('later_value');
});
