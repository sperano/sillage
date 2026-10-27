import { expect, test } from 'vitest';
import { goldenStory } from './testing/stories';
import { conclusionLabel, eventLabel, refLabel, runLabel } from './describe';
import type { CheckConclusion, Event, EventKind } from './model';

const story = goldenStory('implementation-merged');

function event(id: string): Event {
	const e = story.events.find((x) => x.id === id);
	if (!e) throw new Error(`no event ${id}`);
	return e;
}

test('events are named with their details', () => {
	const request = { ...event('comments/5102'), kind: 'request.created' as const };
	expect(eventLabel({ ...request, details: { command: '/kelos' } })).toBe('Request /kelos');
	expect(eventLabel({ ...request, details: { command: '/gordon', agent: 'agent-sol' } })).toBe(
		'Request /gordon for agent-sol'
	);
	expect(eventLabel(event('comments/5101'))).toBe('Run announced: vikunja-widgets-agent-opus-102');
});

test('a Kelos observation names its object, how it was seen and its phase', () => {
	const observed = (details: Record<string, string>): Event => ({
		...event('comments/5102'),
		kind: 'run.observed',
		details
	});
	expect(eventLabel(observed({ object: 'task', change: 'modified', phase: 'Running' }))).toBe(
		'Kelos Task modified, phase Running'
	);
	expect(eventLabel(observed({ object: 'taskrecord', change: 'listed' }))).toBe(
		'Kelos TaskRecord listed'
	);
	expect(eventLabel(observed({}))).toBe('Kelos object observed');
});

test('a kind newer than this build is shown as it is named', () => {
	expect(eventLabel({ ...event('comments/5102'), kind: 'task.archived' as EventKind })).toBe(
		'task.archived'
	);
});

test('a run is named by its Task, its role and its outcome', () => {
	const run = goldenStory('announced-run-only').runs[0];
	expect(run && runLabel(run)).toBe(
		'Run vikunja-widgets-agent-opus-105 (implementation): outcome unknown'
	);
});

test('a correlation end is named by the record it points to', () => {
	expect(refLabel(story, { kind: 'task', id: 'vikunja.example.com/tasks/102' })).toBe(
		'Task 102 (this story)'
	);
	expect(refLabel(story, { kind: 'run', id: 'cluster-b/agents/uid/x' })).toBe(
		'run cluster-b/agents/uid/x'
	);
});

test('check conclusions are named in words, an unknown one as it is', () => {
	expect(conclusionLabel('timed_out')).toBe('timed out');
	expect(conclusionLabel('action_required')).toBe('action required');
	expect(conclusionLabel('success')).toBe('succeeded');
	// A conclusion the Go model gained after this build, which the union cannot name.
	expect(conclusionLabel('later_value' as CheckConclusion)).toBe('later_value');
});
