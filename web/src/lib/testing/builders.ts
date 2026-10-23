// Small records for unit tests: complete enough to be valid, with every
// field a test cares about overridable.

import type { Event, Run, Story } from '../model';

export const TASK = { instance: 'vikunja.example.com', id: 1 };
export const PR = { instance: 'github.example.com', repository: 'example-org/widgets', number: 3 };

let next = 0;

export function event(over: Partial<Event> = {}): Event {
	next += 1;
	return {
		source: 'vikunja',
		instance: 'vikunja.example.com',
		id: `comments/${next}`,
		idKind: 'source',
		kind: 'comment.created',
		sourceTime: '2026-03-01T10:00:00Z',
		observedAt: '2026-03-01T10:01:00Z',
		actor: { login: 'owner', kind: 'person' },
		related: { task: TASK },
		evidence: { source: 'vikunja', instance: 'vikunja.example.com', ref: 'GET /x' },
		...over
	};
}

export function prEvent(kind: Event['kind'], at: string, over: Partial<Event> = {}): Event {
	return event({
		source: 'github',
		instance: 'github.example.com',
		kind,
		sourceTime: at,
		related: { pr: PR },
		evidence: { source: 'github', instance: 'github.example.com', ref: 'GET /y' },
		...over
	});
}

export function run(over: Partial<Run> = {}): Run {
	next += 1;
	return {
		key: { cluster: 'cluster-a', namespace: 'agents', uid: `uid-${next}` },
		taskName: 'task-name',
		role: 'implementation',
		interval: {
			start: { state: 'known', at: '2026-03-01T10:00:00Z' },
			end: { state: 'known', at: '2026-03-01T10:10:00Z' }
		},
		outcome: 'succeeded',
		evidence: [{ source: 'kelos', instance: 'cluster-a', ref: 'tasks/agents/x' }],
		...over
	};
}

export function story(over: Partial<Story> = {}): Story {
	return {
		root: { task: TASK },
		events: [],
		runs: [],
		pullRequests: [],
		correlations: [],
		coverage: [],
		...over
	};
}
