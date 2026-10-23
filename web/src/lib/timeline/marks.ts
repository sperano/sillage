// What each mark says. Every ending has its own mark and text: a task done,
// a run's exit, a check's success, a pull request closed and one merged are
// never drawn as one "done".

import { eventTime, type CheckConclusion, type Event, type Outcome } from '../model';
import { formatFull, type Zone } from './time';

/**
 * The shape of a point on the timeline. Each kind has its own glyph, so
 * that shape and text, not colour alone, tell them apart.
 */
export type MarkKind =
	| 'event'
	| 'observed'
	| 'task-done'
	| 'run-exit'
	| 'check-success'
	| 'check-end'
	| 'pr-merged'
	| 'pr-closed';

/** The mark of an event: its kind's own, or a plain or observed dot. */
export function eventMark(e: Event): MarkKind {
	switch (e.kind) {
		case 'task.done':
			return 'task-done';
		case 'pr.merged':
			return 'pr-merged';
		case 'pr.closed':
			return 'pr-closed';
	}
	return e.sourceTime === undefined ? 'observed' : 'event';
}

function detail(e: Event, name: string): string {
	return e.details?.[name] ?? '';
}

function withDetail(text: string, value: string): string {
	return value ? `${text}: ${value}` : text;
}

/** The short text of an event, as the timeline shows it next to its mark. */
export function eventLabel(e: Event): string {
	const requestee = detail(e, 'reviewer') || detail(e, 'team');
	switch (e.kind) {
		case 'task.created':
			return 'task created';
		case 'task.bucket_observed':
			return `${detail(e, 'bucket') || 'bucket'} observed`;
		case 'task.label_observed':
			return withDetail('label observed', detail(e, 'label'));
		case 'task.assignee_observed':
			return withDetail('assignee observed', detail(e, 'assignee'));
		case 'task.done':
			return 'task done';
		case 'page.created':
			return 'page created';
		case 'page.edited':
			return detail(e, 'version') === 'live' ? 'page edited (live version)' : 'page edited';
		case 'page.moved':
			return 'page moved';
		case 'comment.created':
			return 'comment';
		case 'request.created':
			return withDetail('request', detail(e, 'command'));
		case 'run.announced':
			return 'run announced';
		case 'pr.opened':
			return 'PR opened';
		case 'pr.review_requested':
			return withDetail('review requested', requestee);
		case 'pr.review_request_removed':
			return withDetail('review request removed', requestee);
		case 'pr.review_submitted':
			return withDetail('review', detail(e, 'state'));
		case 'pr.merged':
			return 'PR merged';
		case 'pr.closed':
			return 'PR closed, not merged';
		case 'pr.reopened':
			return 'PR reopened';
		default: {
			// A kind added to the model fails to compile here until it has a label.
			const unlabelled: never = e.kind;
			return unlabelled;
		}
	}
}

/** Who acted, as the account and, when known, the executor behind it. */
export function actorLabel(e: Event): string {
	const account = e.actor.login ?? 'unknown actor';
	return e.executor ? `${account} (executor ${e.executor.name})` : account;
}

/**
 * Everything a mark says, in words: what happened, who, and when, and
 * whether the time is only when the state was observed.
 */
export function eventName(e: Event, zone: Zone): string {
	const observed = e.sourceTime === undefined ? ', observed time' : '';
	return `${eventLabel(e)}, ${actorLabel(e)}, ${formatFull(eventTime(e), zone)}${observed}`;
}

/** The text of a run's exit: how it ended. */
export function runExitLabel(outcome: Outcome): string {
	return `run exit: ${outcome}`;
}

/** The mark and text at the end of a completed check. */
export function checkEnd(
	name: string,
	conclusion: CheckConclusion
): { mark: MarkKind; label: string } {
	return {
		mark: conclusion === 'success' ? 'check-success' : 'check-end',
		label: `check ${name}: ${conclusion.replaceAll('_', ' ')}`
	};
}
