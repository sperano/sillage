// The JSON of internal/overview's Page, as GET /api/v1/overview serves it:
// one page of the rows of the tasks active in a window. Each type mirrors
// its Go struct field for field; decode.ts checks a response against them,
// and decode.test.ts decodes the golden overview
// (testdata/fixtures/overview), so that a change on the Go side fails here.

import type { Coverage, Interval, PRKey, Root, Run } from '../model';

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
	pr?: PRKey;
	/** The pod a start announcement names. */
	pod?: string;
}

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
