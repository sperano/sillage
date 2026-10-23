// The overview's query: its window, its filters and its grouping, as the
// page's URL holds them (so that a filtered overview is a link) and as the
// API takes them.

/** How the overview groups its lanes: a row per task, or a group per agent profile. */
export type Grouping = 'task' | 'executor';

export interface OverviewQuery {
	/** RFC 3339; absent: the API's default, the day before to. */
	from?: string;
	/** RFC 3339; absent: now. */
	to?: string;
	project: string;
	/** Text in a task's title or key. */
	task: string;
	/** An agent profile. */
	executor: string;
	/** Only tasks with a review request waiting in the window. */
	review: boolean;
	/** Only tasks with a failed run. */
	failed: boolean;
	/** Only tasks whose history is missing in part of the window. */
	missing: boolean;
	group: Grouping;
}

/** The overview's path. */
export const OVERVIEW_PATH = '/overview';

/** The query parameters, shared by the page's URL and the API. */
export const PARAMS = {
	from: 'from',
	to: 'to',
	project: 'project',
	task: 'task',
	executor: 'executor',
	review: 'review',
	failed: 'failed',
	missing: 'missing',
	group: 'group',
	after: 'after',
	limit: 'limit'
} as const;

const TRUE = 'true';

/** The query with nothing set: the API's default window, no filter, a row per task. */
export const DEFAULT_QUERY: OverviewQuery = {
	project: '',
	task: '',
	executor: '',
	review: false,
	failed: false,
	missing: false,
	group: 'task'
};

/** The query a URL's parameters hold. */
export function parseOverviewQuery(params: URLSearchParams): OverviewQuery {
	const text = (name: string) => params.get(name)?.trim() ?? '';
	const q: OverviewQuery = {
		project: text(PARAMS.project),
		task: text(PARAMS.task),
		executor: text(PARAMS.executor),
		review: params.get(PARAMS.review) === TRUE,
		failed: params.get(PARAMS.failed) === TRUE,
		missing: params.get(PARAMS.missing) === TRUE,
		group: params.get(PARAMS.group) === 'executor' ? 'executor' : 'task'
	};
	const from = text(PARAMS.from);
	const to = text(PARAMS.to);
	if (from) q.from = from;
	if (to) q.to = to;
	return q;
}

/** The parameters of the query's filters and window, without what is unset. */
function filterParams(q: OverviewQuery): URLSearchParams {
	const p = new URLSearchParams();
	if (q.from) p.set(PARAMS.from, q.from);
	if (q.to) p.set(PARAMS.to, q.to);
	for (const name of ['project', 'task', 'executor'] as const) {
		if (q[name]) p.set(PARAMS[name], q[name]);
	}
	for (const name of ['review', 'failed', 'missing'] as const) {
		if (q[name]) p.set(PARAMS[name], TRUE);
	}
	return p;
}

/** The overview page's URL for the query. */
export function overviewHref(q: OverviewQuery): string {
	const p = filterParams(q);
	if (q.group === 'executor') p.set(PARAMS.group, 'executor');
	const search = p.toString();
	return search ? `${OVERVIEW_PATH}?${search}` : OVERVIEW_PATH;
}

/** The API's parameters for one page of the query: after the cursor, when one is given. */
export function overviewApiParams(q: OverviewQuery, after?: string): URLSearchParams {
	const p = filterParams(q);
	if (after) p.set(PARAMS.after, after);
	return p;
}
