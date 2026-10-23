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

/**
 * The review requests of one open pull request that nothing has answered
 * or withdrawn. GitHub keeps a request across a close and a reopen: a
 * closed pull request has none pending until it is reopened.
 */
function pendingReviews(story: Story, pr: PRKey): NextAction[] {
	const key = prKeyString(pr);
	const pending = new Map<string, Extract<NextAction, { kind: 'review' }>>();
	let closed = false;
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
				pending.clear();
				break;
			case 'pr.closed':
				closed = true;
				break;
			case 'pr.reopened':
				closed = false;
				break;
		}
	}
	return closed ? [] : [...pending.values()];
}

/**
 * The Kelos Task names of the follow-up spawners that end in the id of the
 * comment or review they answer, as internal/collector's names.go reads
 * them: vikunja-fu-…-c<id>, wiki-fu-…-c<id>, pr-followup-…-<c|l|r|p><id>,
 * each with an optional retry suffix.
 */
const answeringName =
	/^(?:vikunja-fu|wiki-fu|pr-followup)-.*-[clrp]([1-9][0-9]*)(?:-retry[1-9][0-9]*)?$/;

/** The id of the record an announcement's Task name says it answers, if it names one. */
function answeredID(announcement: Event): string | undefined {
	return answeringName.exec(announcement.details?.taskName ?? '')?.[1];
}

/** The request's own id: the number its event id ends in ("issues/comments/5" → "5"). */
function requestID(request: Event): string | undefined {
	return /\/([0-9]+)$/.exec(request.id)?.[1];
}

/**
 * Whether the announcement can be the answer to the request: posted on the
 * same record at or after it, and naming it, or naming no request at all,
 * which leaves it unknown which earlier request it answers. One naming
 * another request is not this one's answer.
 */
function mayAnswer(announcement: Event, request: Event): boolean {
	const where = thread(request.related);
	if (where === undefined || thread(announcement.related) !== where) {
		return false;
	}
	if (eventTime(announcement) < eventTime(request)) {
		return false;
	}
	const named = answeredID(announcement);
	return named === undefined || named === requestID(request);
}

/** Whether a run is linked to the request, or an announcement may be its answer. */
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
	return story.events.some((e) => e.kind === 'run.announced' && mayAnswer(e, request));
}

export function nextActions(story: Story): NextAction[] {
	const reviews = story.pullRequests.flatMap((pr) => pendingReviews(story, pr.key));
	const agents: NextAction[] = story.events
		.filter((e) => e.kind === 'request.created' && !answered(story, e))
		.map((request) => ({ kind: 'agent', request }));
	return [...reviews, ...agents];
}
