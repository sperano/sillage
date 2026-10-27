import { describe, expect, test } from 'vitest';
import { goldenStories, goldenStory } from './testing/stories';
import { nextActions } from './nextAction';
import type { Event, EventKind, Story } from './model';
import { runKeyString } from './keys';

const PR = { instance: 'github.example.com', repository: 'example-org/widgets', number: 7 };

let seq = 0;

/** A fabricated GitHub event on the merged story's pull request, at minute m after 20:00. */
function github(
	kind: EventKind,
	m: number,
	login: string,
	details?: Record<string, string>
): Event {
	seq += 1;
	const at = `2026-03-03T20:${String(m).padStart(2, '0')}:00Z`;
	return {
		source: 'github',
		instance: PR.instance,
		id: `issues/events/${90000 + seq}`,
		idKind: 'source',
		kind,
		sourceTime: at,
		observedAt: at,
		actor: { login, kind: 'person' },
		related: { pr: PR },
		details,
		evidence: {
			source: 'github',
			instance: PR.instance,
			ref: 'GET /repos/example-org/widgets/issues/7/timeline'
		}
	};
}

/** The merged story without its merge, plus events. */
function openPR(...events: Event[]): Story {
	const story = goldenStory('implementation-merged');
	story.events = [...story.events.filter((e) => e.kind !== 'pr.merged'), ...events];
	return story;
}

describe('the golden stories', () => {
	test.each([...goldenStories])('%s: unknown, nothing explicit is pending', (_, story) => {
		expect(nextActions(story)).toEqual([]);
	});
});

describe('review requests', () => {
	test('a pending request names the reviewer', () => {
		const requested = github('pr.review_requested', 1, 'owner-gh', { reviewer: 'reviewer-a' });
		expect(nextActions(openPR(requested))).toEqual([
			{ kind: 'review', reviewer: 'reviewer-a', team: false, pr: PR, requested }
		]);
	});

	test('a team request names the team', () => {
		const actions = nextActions(
			openPR(github('pr.review_requested', 1, 'owner-gh', { team: 'core' }))
		);
		expect(actions).toMatchObject([{ kind: 'review', reviewer: 'core', team: true }]);
	});

	test.each([
		['withdrawn', github('pr.review_request_removed', 2, 'owner-gh', { reviewer: 'reviewer-a' })],
		[
			'answered by the reviewer',
			github('pr.review_submitted', 2, 'reviewer-a', { state: 'approved' })
		],
		['merged', github('pr.merged', 2, 'owner-gh')],
		['closed', github('pr.closed', 2, 'owner-gh')]
	])('a request %s is not pending', (_, after) => {
		const requested = github('pr.review_requested', 1, 'owner-gh', { reviewer: 'reviewer-a' });
		expect(nextActions(openPR(requested, after))).toEqual([]);
	});

	test('a request survives a close and a reopen', () => {
		const requested = github('pr.review_requested', 1, 'owner-gh', { reviewer: 'reviewer-a' });
		const actions = nextActions(
			openPR(requested, github('pr.closed', 2, 'owner-gh'), github('pr.reopened', 3, 'owner-gh'))
		);
		expect(actions).toEqual([
			{ kind: 'review', reviewer: 'reviewer-a', team: false, pr: PR, requested }
		]);
	});

	test("someone else's review leaves a user's request pending", () => {
		const actions = nextActions(
			openPR(
				github('pr.review_requested', 1, 'owner-gh', { reviewer: 'reviewer-a' }),
				github('pr.review_submitted', 2, 'reviewer-b', { state: 'commented' })
			)
		);
		expect(actions).toMatchObject([{ kind: 'review', reviewer: 'reviewer-a' }]);
	});

	test("any review leaves a team's request no longer known to be pending", () => {
		const actions = nextActions(
			openPR(
				github('pr.review_requested', 1, 'owner-gh', { team: 'core' }),
				github('pr.review_submitted', 2, 'reviewer-b', { state: 'approved' })
			)
		);
		expect(actions).toEqual([]);
	});
});

describe('agent requests', () => {
	const story = goldenStory('failed-run-retry');
	const announcement = story.events.find((e) => e.kind === 'run.announced');
	if (!announcement) throw new Error('no announcement');

	function request(at: string): Event {
		return {
			...announcement!,
			id: 'comments/5499',
			kind: 'request.created',
			sourceTime: at,
			observedAt: at,
			actor: { login: 'owner', kind: 'person' },
			details: { command: '/gordon' }
		};
	}

	test('a request with no announcement after it is waiting for an agent', () => {
		const s = goldenStory('failed-run-retry');
		const r = request('2026-03-05T14:30:00Z');
		s.events.push(r);
		expect(nextActions(s)).toEqual([{ kind: 'agent', request: r }]);
	});

	test('a request with an announcement after it on the same task is answered', () => {
		const s = goldenStory('failed-run-retry');
		s.events.push(request('2026-03-05T14:00:00Z'));
		expect(nextActions(s)).toEqual([]);
	});

	test('an announcement after it on another task leaves it waiting', () => {
		const s = goldenStory('failed-run-retry');
		const r = request('2026-03-05T14:00:00Z');
		s.events.push(r);
		for (const e of s.events.filter((x) => x.kind === 'run.announced')) {
			e.related = { task: { instance: 'vikunja.example.com', id: 105 } };
		}
		expect(nextActions(s)).toEqual([{ kind: 'agent', request: r }]);
	});

	/** The story with only its last announcement, posted by the follow-up Task named name. */
	function announcedAs(name: string): Story {
		const s = goldenStory('failed-run-retry');
		const last = s.events.filter((e) => e.kind === 'run.announced').at(-1);
		if (!last) throw new Error('no announcement');
		s.events = s.events.filter((e) => e.kind !== 'run.announced' || e === last);
		last.details = { ...last.details, taskName: name };
		return s;
	}

	test.each([
		'vikunja-fu-widgets-104-c5499',
		'vikunja-fu-widgets-104-c5499-retry1',
		'pr-followup-widgets-7-r5499',
		'pr-followup-widgets-7-l5499-retry2'
	])('an announcement whose Task name %s names the request answers it', (name) => {
		const s = announcedAs(name);
		s.events.push(request('2026-03-05T14:00:00Z'));
		expect(nextActions(s)).toEqual([]);
	});

	test.each(['vikunja-fu-widgets-104-c5500', 'pr-followup-widgets-7-c54990'])(
		'an announcement whose Task name %s names another request leaves it waiting',
		(name) => {
			const s = announcedAs(name);
			const r = request('2026-03-05T14:00:00Z');
			s.events.push(r);
			expect(nextActions(s)).toEqual([{ kind: 'agent', request: r }]);
		}
	);

	test('an announcement whose Task name names no request answers an earlier one', () => {
		const s = announcedAs('vikunja-widgets-agent-opus-104');
		s.events.push(request('2026-03-05T14:00:00Z'));
		expect(nextActions(s)).toEqual([]);
	});

	/** A time Date.parse cannot read: the order it is in is unknown. */
	const UNREADABLE_TIME = 'not a time';

	test('an announcement without a time may answer a request', () => {
		const s = announcedAs('vikunja-widgets-agent-opus-104');
		const announced = s.events.find((e) => e.kind === 'run.announced');
		if (!announced) throw new Error('no announcement');
		announced.sourceTime = UNREADABLE_TIME;
		s.events.push(request('2026-03-05T15:00:00Z'));
		expect(nextActions(s)).toEqual([]);
	});

	test('an announcement may answer a request without a time', () => {
		const s = announcedAs('vikunja-widgets-agent-opus-104');
		s.events.push(request(UNREADABLE_TIME));
		expect(nextActions(s)).toEqual([]);
	});

	test('an announcement known to be before it leaves it waiting', () => {
		const s = announcedAs('vikunja-widgets-agent-opus-104');
		const r = request('2026-03-05T15:00:00Z');
		s.events.push(r);
		expect(nextActions(s)).toEqual([{ kind: 'agent', request: r }]);
	});

	test('a request a correlation links to a run is answered', () => {
		const s = goldenStory('failed-run-retry');
		s.events = s.events.filter((e) => e.kind !== 'run.announced');
		const r = request('2026-03-05T14:00:00Z');
		s.events.push(r);
		s.correlations.push({
			from: { kind: 'event', id: `${r.source}/${r.instance}/${r.id}` },
			to: { kind: 'run', id: runKeyString(s.runs[0]!.key) },
			confidence: 'identifier',
			evidence: [r.evidence]
		});
		expect(nextActions(s)).toEqual([]);
	});
});
