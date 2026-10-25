// The JSON of internal/overview's Page, as GET /api/v1/overview serves it:
// one page of the rows of the tasks active in a window. Each type mirrors
// its Go struct field for field; decode.ts checks a response against them,
// and decode.test.ts decodes the golden overview
// (testdata/fixtures/overview), so that a change on the Go side fails here.

import type { Coverage, Interval, PRKey, Root, Run, Source } from '../model';

/** A pull request's state at the end of the window. */
export const PR_STATES = ['open', 'merged', 'closed', 'unknown'] as const;
export type PRState = (typeof PR_STATES)[number];

/** The span an overview covers: from inclusive, to exclusive. */
export interface Window {
	from: string;
	to: string;
}

/** One pull request of a row: when it was open, and its state at the window's end. */
export interface OverviewPullRequest {
	key: PRKey;
	branch: string;
	/** Each opening or reopening to its merge or close, in time order. */
	periods: Interval[];
	/** closed: without a merge. Its task stays as Vikunja says. */
	state: PRState;
}

/** A review request until the requested reviewer's review or the request's removal. */
export interface ReviewWait {
	pr: PRKey;
	reviewer?: string;
	team?: string;
	/** The request event's key. */
	request: string;
	interval: Interval;
	/**
	 * The merge, or the close not followed by a reopening, that left the
	 * request unanswered: waiting until then, its answer unknown after.
	 * Set exactly when the interval's end is unknown.
	 */
	closed?: string;
}

/** The event kinds a row shows as marks, as the Go overview's markKinds. */
export const MARK_KINDS = [
	'task.done',
	'run.announced',
	'pr.opened',
	'pr.merged',
	'pr.closed',
	'pr.reopened',
	'pr.review_submitted'
] as const;
export type MarkKind = (typeof MARK_KINDS)[number];

/** One state change in the window: a completion, a pull request's, a review, an announcement. */
export interface Mark {
	kind: MarkKind;
	at: string;
	/** The event's key. */
	event: string;
	/** Where the event was read: its coverage says whether the row's history there is known. */
	source: Source;
	instance: string;
	pr?: PRKey;
	/** The pod a start announcement names. */
	pod?: string;
	/** The namespace a start announcement states. */
	namespace?: string;
}

/** A task's completion states, as the Go model's CompletionState. */
export const COMPLETION_STATES = ['done', 'reopened'] as const;
export type CompletionState = (typeof COMPLETION_STATES)[number];

/**
 * What the records say of a reopening: Vikunja records no time for it,
 * only that it came after one time and no later than another.
 */
export interface Reopening {
	/** The latest of the completion and the reads after it that still saw the task done. */
	after: string;
	/** The first read after that which saw the task not done. */
	by: string;
	/** The key of that read's completion observation. */
	observation: string;
}

interface CompletionFields {
	/** The task.done event's key, and its time. */
	done: string;
	doneAt: string;
}

/** A task's latest completion before the window's end, and whether it was reopened since. */
export type TaskCompletion = CompletionFields &
	({ state: 'done'; reopened?: never } | { state: 'reopened'; reopened: Reopening });

/** One task or page in the window. */
export interface OverviewRow {
	root: Root;
	title?: string;
	/** The project labels of all the root's runs. */
	projects?: string[];
	/** The runs in the window, and each earlier failed attempt of a retry in it. */
	runs: Run[];
	pullRequests: OverviewPullRequest[];
	reviewWaits: ReviewWait[];
	marks: Mark[];
	/**
	 * The task's completion at the window's end, when it changed in the
	 * window: completed in it, or seen reopened in it.
	 */
	completion?: TaskCompletion;
	coverage: Coverage[];
	/** Part of the window is unknown for one of the row's sources. */
	missingHistory: boolean;
	/** The latest time in the window the row shows anything; rows are ordered by it. */
	lastActivity: string;
}

/** One page of an overview. */
export interface OverviewPage {
	window: Window;
	rows: OverviewRow[];
	/** How many rows match, on every page. */
	total: number;
	/** The cursor of the next page; absent on the last. */
	next?: string;
}
