import type { Event, PRKey, Related, Story } from './model';
import { eventKey, pageKeyString, prKeyString, taskKeyString } from './keys';

/**
 * Who or what has the next move, from explicit evidence only: a review
 * request still pending, or a request no agent has announced it is on. A
 * quiet period never names anyone; with no such evidence the next action
 * is unknown (an empty list).
 */
export type NextAction =
	| { kind: 'review'; reviewer: string; team: boolean; pr: PRKey; requested: Event }
	| { kind: 'agent'; request: Event };

function eventTime(e: Event): number {
	return Date.parse(e.sourceTime ?? e.observedAt);
}

/** The record a comment is on: its pull request, else its page, else its task. */
function thread(r: Related): string | undefined {
	if (r.pr) {
		return `pr:${prKeyString(r.pr)}`;
	}
	if (r.page) {
		return `page:${pageKeyString(r.page)}`;
	}
	return r.task && `task:${taskKeyString(r.task)}`;
}

/** The review requests of one pull request that nothing has answered or withdrawn. */
function pendingReviews(story: Story, pr: PRKey): NextAction[] {
	const key = prKeyString(pr);
	const pending = new Map<string, Extract<NextAction, { kind: 'review' }>>();
	for (const e of story.events) {
		if (e.source !== 'github' || !e.related.pr || prKeyString(e.related.pr) !== key) {
			continue;
		}
		const team = !e.details?.reviewer;
		const who = e.details?.reviewer ?? e.details?.team;
		const id = who && `${team ? 'team' : 'user'}:${who}`;
		switch (e.kind) {
			case 'pr.review_requested':
				if (who && id) {
					pending.set(id, { kind: 'review', reviewer: who, team, pr, requested: e });
				}
				break;
			case 'pr.review_request_removed':
				if (id) {
					pending.delete(id);
				}
				break;
			case 'pr.review_submitted':
				pending.delete(`user:${e.actor.login ?? ''}`);
				// Whether a member's review answered a team's request is not
				// recorded: the request is no longer known to be pending.
				for (const k of [...pending.keys()].filter((k) => k.startsWith('team:'))) {
					pending.delete(k);
				}
				break;
			case 'pr.merged':
			case 'pr.closed':
				pending.clear();
				break;
		}
	}
	return [...pending.values()];
}

/** Whether a run is linked to the request, or announced after it on the same record. */
function answered(story: Story, request: Event): boolean {
	const key = eventKey(request);
	const linked = story.correlations.some(
		(c) =>
			(c.from.kind === 'event' && c.from.id === key && c.to.kind === 'run') ||
			(c.to.kind === 'event' && c.to.id === key && c.from.kind === 'run')
	);
	if (linked) {
		return true;
	}
	const where = thread(request.related);
	return story.events.some(
		(e) =>
			e.kind === 'run.announced' &&
			where !== undefined &&
			thread(e.related) === where &&
			eventTime(e) >= eventTime(request)
	);
}

export function nextActions(story: Story): NextAction[] {
	const reviews = story.pullRequests.flatMap((pr) => pendingReviews(story, pr.key));
	const agents: NextAction[] = story.events
		.filter((e) => e.kind === 'request.created' && !answered(story, e))
		.map((request) => ({ kind: 'agent', request }));
	return [...reviews, ...agents];
}
