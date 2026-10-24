import { expect, test } from 'vitest';
import { event, prEvent } from '../testing/builders';
import { actorLabel, checkEnd, eventLabel, eventMark, runExitLabel } from './marks';

test('task done, run exit, check success, PR closed and PR merged each have their own mark', () => {
	const marks = [
		eventMark(event({ kind: 'task.done' })),
		'run-exit',
		checkEnd('test', 'success').mark,
		eventMark(prEvent('pr.closed', '2026-03-01T10:00:00Z')),
		eventMark(prEvent('pr.merged', '2026-03-01T10:00:00Z'))
	];
	expect(new Set(marks).size).toBe(marks.length);
	expect(marks).not.toContain('event');
});

test('an observed state is marked as observed and says so', () => {
	const e = event({
		kind: 'task.bucket_observed',
		sourceTime: undefined,
		details: { bucket: 'Doing' }
	});
	expect(eventMark(e)).toBe('observed');
	expect(eventLabel(e)).toBe('Doing observed');
});

test('labels name what ended', () => {
	expect(eventLabel(prEvent('pr.closed', '2026-03-01T10:00:00Z'))).toBe('PR closed, not merged');
	expect(eventLabel(event({ kind: 'page.edited', details: { version: 'live' } }))).toBe(
		'page edited (live version)'
	);
	expect(runExitLabel('failed')).toBe('run exit: failed');
	expect(checkEnd('build', 'timed_out')).toEqual({
		mark: 'check-end',
		label: 'check build: timed out'
	});
});

test('the actor keeps account and executor apart', () => {
	const e = prEvent('pr.opened', '2026-03-01T10:00:00Z', {
		actor: { login: 'owner-gh', kind: 'person' },
		executor: { name: 'agent-opus', kind: 'agent' }
	});
	expect(actorLabel(e)).toBe('owner-gh (executor agent-opus)');
});

test('a Kelos observation is marked as observed and names its object, change and phase', () => {
	const e = event({
		source: 'kelos',
		instance: 'cluster-a',
		kind: 'run.observed',
		sourceTime: undefined,
		details: { object: 'task', change: 'modified', phase: 'Running' }
	});
	expect(eventMark(e)).toBe('observed');
	expect(eventLabel(e)).toBe('task modified: Running');
	expect(eventLabel({ ...e, details: {} })).toBe('object observed');
});
