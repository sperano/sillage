// The timeline's rows: the root task or page, people, agent runs (one row
// per attempt), automation, pull requests with their checks per commit, and
// wiki pages. Each event is drawn on exactly one row.

import { eventKey, pageKeyString, prKeyString, runKeyString, taskKeyString } from '../keys';
import {
	boundTime,
	coverageKey,
	eventTime,
	type Event,
	type PageKey,
	type PRKey,
	type Run,
	type Story,
	type TaskKey
} from '../model';
import { checkSpan, hasExtent, reviewWaits, runSpans, type Span } from './intervals';

/** The lanes, in the order they are drawn after the root's lane. */
export const SECTIONS = ['task', 'people', 'runs', 'automation', 'pulls', 'pages'] as const;
export type Section = (typeof SECTIONS)[number];

export const SECTION_TITLES: Record<Section, string> = {
	task: 'Task',
	people: 'People',
	runs: 'Agent runs',
	automation: 'Automation',
	pulls: 'Pull requests',
	pages: 'Wiki'
};

/** The number of a commit's leading hex digits a check row shows. */
const SHORT_SHA_LENGTH = 7;

export interface Row {
	id: string;
	section: Section;
	title: string;
	subtitle: string;
	/** The coverage keys of the sources the row's records come from. */
	sources: string[];
	events: Event[];
	spans: Span[];
	/** Said on the row about what it cannot draw: an interval with no known end. */
	note?: string;
	/** Where the row sorts within its section. */
	order: number;
	/** The row this one is drawn under: a pull request's, for its checks. */
	parent?: string;
}

/** What a row is created with; the rest fills in as records are placed. */
type RowInit = Omit<Row, 'id' | 'events' | 'spans' | 'order'>;

/** The kinds of event a conversation posts, drawn on their author's row. */
const CONVERSATION = new Set<Event['kind']>([
	'comment.created',
	'request.created',
	'run.announced'
]);

/** Builds the rows of a story, in drawing order. */
export function buildRows(story: Story): Row[] {
	const rows = new Map<string, Row>();
	const lastRead = lastReads(story);
	const ensure = (id: string, init: () => RowInit) => {
		let row = rows.get(id);
		if (!row) {
			row = { id, events: [], spans: [], order: Number.POSITIVE_INFINITY, ...init() };
			rows.set(id, row);
		}
		return row;
	};
	const ctx: Context = {
		story,
		ensure,
		find: (id) => rows.get(id),
		lastRead,
		runOfEvent: runOfEvent(story)
	};
	addRoot(ctx);
	addRuns(ctx);
	addPullRequests(ctx);
	for (const e of story.events) placeEvent(ctx, e);
	addReviewWaits(ctx, [...rows.values()]);
	for (const row of rows.values()) addNote(row);
	return sortRows([...rows.values()], story.root.task ? 'task' : 'pages');
}

interface Context {
	story: Story;
	ensure: (id: string, init: () => RowInit) => Row;
	find: (id: string) => Row | undefined;
	lastRead: (source: string) => number | undefined;
	runOfEvent: Map<string, string>;
}

function lastReads(story: Story): (key: string) => number | undefined {
	const reads = new Map<string, number>();
	for (const c of story.coverage) {
		if (c.lastSuccessfulRead) {
			reads.set(coverageKey(c.source, c.instance), Date.parse(c.lastSuccessfulRead));
		}
	}
	return (key) => reads.get(key);
}

/**
 * The run each event belongs to, when an identifier correlation links it
 * to exactly one run. A heuristic or ambiguous link never moves an event.
 */
function runOfEvent(story: Story): Map<string, string> {
	const runs = new Map<string, Set<string>>();
	for (const c of story.correlations) {
		if (c.confidence !== 'identifier' || c.ambiguous) continue;
		const [event, run] =
			c.from.kind === 'event' && c.to.kind === 'run'
				? [c.from.id, c.to.id]
				: c.to.kind === 'event' && c.from.kind === 'run'
					? [c.to.id, c.from.id]
					: [];
		if (event === undefined || run === undefined) continue;
		runs.set(event, (runs.get(event) ?? new Set()).add(run));
	}
	const out = new Map<string, string>();
	for (const [event, set] of runs) {
		if (set.size === 1) out.set(event, [...set][0]!);
	}
	return out;
}

function taskRow(ctx: Context, key: TaskKey): Row {
	return ctx.ensure(`task:${taskKeyString(key)}`, () => ({
		section: 'task',
		title: `Task ${key.id}`,
		subtitle: key.instance,
		sources: [coverageKey('vikunja', key.instance)]
	}));
}

function pageRow(ctx: Context, key: PageKey): Row {
	return ctx.ensure(`page:${pageKeyString(key)}`, () => ({
		section: 'pages',
		title: `Page ${key.id}`,
		subtitle: `${key.instance} · ${key.locale}`,
		sources: [coverageKey('wiki', key.instance)]
	}));
}

function prRow(ctx: Context, key: PRKey): Row {
	return ctx.ensure(`pr:${prKeyString(key)}`, () => ({
		section: 'pulls',
		title: `#${key.number} ${key.repository}`,
		subtitle: 'pull request',
		sources: [coverageKey('github', key.instance)]
	}));
}

function addRoot(ctx: Context): void {
	const { task, page } = ctx.story.root;
	if (task) taskRow(ctx, task).order = Number.NEGATIVE_INFINITY;
	if (page) pageRow(ctx, page).order = Number.NEGATIVE_INFINITY;
}

/**
 * One row per run: a retry is a separate attempt, never merged. Runs are
 * numbered as attempts of one name only within a cluster and namespace: a
 * name is a label, not an identity, so the same name elsewhere is another
 * task, and its rows name their cluster and namespace.
 */
function addRuns(ctx: Context): void {
	const byScope = new Map<string, { name: string; attempts: Run[] }>();
	const scopesOfName = new Map<string, number>();
	for (const run of ctx.story.runs) {
		const name = run.taskName ?? run.pod ?? runKeyString(run.key);
		const scope = JSON.stringify([run.key.cluster, run.key.namespace, name]);
		const entry = byScope.get(scope);
		if (entry) entry.attempts.push(run);
		else {
			byScope.set(scope, { name, attempts: [run] });
			scopesOfName.set(name, (scopesOfName.get(name) ?? 0) + 1);
		}
	}
	for (const { name, attempts } of byScope.values()) {
		attempts.sort((a, b) => runAnchor(a) - runAnchor(b));
		const scoped = (scopesOfName.get(name) ?? 0) > 1;
		attempts.forEach((run, i) =>
			addRun(ctx, run, { name, attempt: attempts.length > 1 ? i + 1 : 0, scoped })
		);
	}
}

/** The run's first known time, to order attempts. */
function runAnchor(run: Run): number {
	const times = [run.created, run.interval.start.at, run.interval.end.at];
	const known = times.filter((t) => t !== undefined).map((t) => Date.parse(t));
	return known.length > 0 ? Math.min(...known) : Number.POSITIVE_INFINITY;
}

/**
 * How a run's row is titled: its name, its attempt (0 for a run with no
 * retry), and whether runs of the same name elsewhere call for its cluster
 * and namespace.
 */
interface RunTitle {
	name: string;
	attempt: number;
	scoped: boolean;
}

function addRun(ctx: Context, run: Run, { name, attempt, scoped }: RunTitle): void {
	const source = coverageKey('kelos', run.key.cluster);
	const scope = scoped ? `${run.key.cluster}/${run.key.namespace}` : '';
	const row = ctx.ensure(`run:${runKeyString(run.key)}`, () => ({
		section: run.role === 'automation' ? 'automation' : 'runs',
		title: name,
		subtitle: [run.role, scope, attempt > 0 ? `attempt ${attempt}` : '', run.outcome]
			.filter(Boolean)
			.join(' · '),
		sources: [source]
	}));
	row.order = runAnchor(run);
	row.spans.push(...runSpans(run, ctx.lastRead(source)));
}

/** One row per pull request, and one row of checks per commit. */
function addPullRequests(ctx: Context): void {
	for (const pr of ctx.story.pullRequests) {
		const row = prRow(ctx, pr.key);
		row.subtitle = pr.branch;
		const lastRead = ctx.lastRead(row.sources[0]!);
		for (const check of pr.checks) {
			const checks = ctx.ensure(`checks:${prKeyString(pr.key)}:${check.sha}`, () => ({
				section: 'pulls',
				title: `checks ${check.sha.slice(0, SHORT_SHA_LENGTH)}`,
				subtitle: `#${pr.key.number}, commit ${check.sha.slice(0, SHORT_SHA_LENGTH)}`,
				sources: row.sources,
				parent: row.id
			}));
			checks.spans.push(checkSpan(pr.key, check, lastRead));
			checks.order = Math.min(
				checks.order,
				boundTime(check.interval.start) ?? Number.POSITIVE_INFINITY
			);
		}
	}
}

/** Draws an event on its row, creating the row on first use. */
function placeEvent(ctx: Context, e: Event): void {
	const row = rowOf(ctx, e);
	row.events.push(e);
	row.order = Math.min(row.order, eventTime(e));
	const source = coverageKey(e.source, e.instance);
	if (!row.sources.includes(source)) row.sources.push(source);
	if (e.kind === 'page.created' || e.kind === 'page.moved') {
		const path = e.kind === 'page.moved' ? e.details?.to : e.details?.path;
		if (path && row.section === 'pages') row.title = path;
	}
}

function rowOf(ctx: Context, e: Event): Row {
	if (e.kind === 'run.observed' && e.related.run) {
		const runRow = ctx.find(`run:${runKeyString(e.related.run)}`);
		if (runRow) return runRow;
	}
	if (CONVERSATION.has(e.kind)) {
		const run = ctx.runOfEvent.get(eventKey(e));
		const runRow = run === undefined ? undefined : ctx.find(`run:${run}`);
		if (runRow && e.actor.kind !== 'person') return runRow;
		if (e.actor.kind !== 'unknown' && e.actor.login) return accountRow(ctx, e, e.actor.login);
	}
	return entityRow(ctx, e);
}

/** The row of the account that posted a conversation event. */
function accountRow(ctx: Context, e: Event, login: string): Row {
	const section =
		e.actor.kind === 'person' ? 'people' : e.actor.kind === 'agent' ? 'runs' : 'automation';
	return ctx.ensure(`account:${e.actor.kind}:${e.source}/${e.instance}:${login}`, () => ({
		section,
		title: login,
		subtitle:
			e.actor.kind === 'agent'
				? `agent account on ${e.instance}; run not established`
				: `${e.actor.kind} on ${e.instance}`,
		sources: []
	}));
}

/** The row of the task, page or pull request the event is about. */
function entityRow(ctx: Context, e: Event): Row {
	const { task, page, pr } = e.related;
	if (pr) return prRow(ctx, pr);
	if (page && (e.source === 'wiki' || !task)) return pageRow(ctx, page);
	if (task) return taskRow(ctx, task);
	const root = ctx.story.root;
	return root.task ? taskRow(ctx, root.task) : pageRow(ctx, root.page);
}

/**
 * What is known of an interval with nothing to draw: no start, and an end
 * that is unknown, or open with no last read to run it to. Undefined for
 * an interval that has a bar or a stub.
 */
function undrawnEnds(span: Span): string | undefined {
	if (hasExtent(span)) return undefined;
	return span.endState === 'open'
		? 'start unknown, not ended at the last read'
		: 'start and end unknown';
}

/**
 * Notes each interval with nothing to draw: without the note it would
 * look like no interval at all.
 */
function addNote(row: Row): void {
	const notes = row.spans.flatMap((s) => {
		const ends = undrawnEnds(s);
		if (ends === undefined) return [];
		return [s.selected.kind === 'run' ? `${ends}: no bar` : `${s.text}: ${ends}, no bar`];
	});
	if (notes.length > 0) row.note = notes.join('; ');
}

function addReviewWaits(ctx: Context, rows: Row[]): void {
	for (const row of rows) {
		if (!row.id.startsWith('pr:')) continue;
		row.spans.push(...reviewWaits(row.events, ctx.lastRead(row.sources[0]!)));
	}
}

/** Compares rows by order, then id; two infinite orders compare equal. */
function byOrder(a: Row, b: Row): number {
	return (a.order === b.order ? 0 : a.order < b.order ? -1 : 1) || a.id.localeCompare(b.id);
}

/**
 * Sorts rows by section, the root's first, then by order, each child right
 * under its parent.
 */
function sortRows(rows: Row[], rootSection: Section): Row[] {
	const section = (r: Row) => (r.section === rootSection ? -1 : SECTIONS.indexOf(r.section));
	const children = rows.filter((r) => r.parent !== undefined).sort(byOrder);
	return rows
		.filter((r) => r.parent === undefined)
		.sort((a, b) => section(a) - section(b) || byOrder(a, b))
		.flatMap((r) => [r, ...children.filter((c) => c.parent === r.id)]);
}
