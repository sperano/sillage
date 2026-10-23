// The lanes of the overview: a task's row collapsed into one lane, or
// expanded into one per run, pull request and review wait; or, grouped by
// executor, one lane per run under its agent profile, so that two runs of
// one profile at the same time stay two lanes.

import { boundTime, type Root, type Run } from '../model';
import { prKeyString } from '../keys';
import type { Mark, OverviewRow } from './model';
import {
	announcedRun,
	prBars,
	requestee,
	runBars,
	runLaneKey,
	waitBar,
	type OverviewSpan
} from './spans';

export interface Lane {
	key: string;
	/** What the lane is: shown beside it when expanded, its accessible name always. */
	label: string;
	spans: OverviewSpan[];
	marks: Mark[];
	/** The row the lane belongs to, when it is shown outside it: grouped by executor. */
	owner?: OverviewRow;
	/** Why nothing is drawn, when nothing can be. */
	note?: string;
}

/** The number of each run among the runs of its Kelos Task, when there are several. */
export function attempts(runs: readonly Run[]): Map<Run, number> {
	const byName = new Map<string, Run[]>();
	for (const r of runs) {
		if (r.taskName === undefined) continue;
		const scope = JSON.stringify([r.key.cluster, r.key.namespace, r.taskName]);
		byName.set(scope, [...(byName.get(scope) ?? []), r]);
	}
	const out = new Map<Run, number>();
	for (const group of byName.values()) {
		if (group.length < 2) continue;
		[...group].sort((a, b) => anchor(a) - anchor(b)).forEach((r, i) => out.set(r, i + 1));
	}
	return out;
}

/** A run's first recorded time; runs without one sort last. */
function anchor(r: Run): number {
	const created = r.created === undefined ? undefined : Date.parse(r.created);
	return created ?? boundTime(r.interval.start) ?? boundTime(r.interval.end) ?? Infinity;
}

/** A run's label: its profile, its attempt and how it ended. */
export function runLabel(run: Run, attempt: number | undefined): string {
	const parts = [run.agent ?? 'unknown executor'];
	if (attempt !== undefined) parts.push(`attempt ${attempt}`);
	parts.push(run.outcome);
	return parts.join(' · ');
}

const NO_BAR_NOTE = 'start and end unknown: no bar';

function runLane(row: OverviewRow, run: Run, label: string, owner?: OverviewRow): Lane {
	const spans = runBars(row, run);
	const drawable = spans.some((s) => s.start !== undefined || s.end !== undefined || s.openUntil);
	return {
		key: runLaneKey(run),
		label,
		spans,
		marks: row.marks.filter((m) => announcedRun(row, m) === run),
		owner,
		note: drawable ? undefined : NO_BAR_NOTE
	};
}

/**
 * The row in one lane: its pull requests' open periods under its review
 * waits under its runs, and every mark. Labels stay sparse there.
 */
export function collapsedLane(row: OverviewRow): Lane {
	return {
		key: 'all',
		label: 'runs, pull requests and review waits',
		spans: [
			...row.pullRequests.flatMap((p) => prBars(row, p)),
			...row.reviewWaits.map((w) => waitBar(row, w)),
			...row.runs.flatMap((r) => runBars(row, r))
		],
		marks: row.marks
	};
}

/** The row expanded: a lane per run, per pull request, per review wait, and the task's own marks. */
export function expandedLanes(row: OverviewRow): Lane[] {
	const numbers = attempts(row.runs);
	const lanes = row.runs.map((r) => runLane(row, r, runLabel(r, numbers.get(r))));
	for (const pr of row.pullRequests) {
		lanes.push({
			key: `pr:${prKeyString(pr.key)}`,
			label: `PR #${pr.key.number} · ${pr.key.repository}`,
			spans: prBars(row, pr),
			marks: row.marks.filter((m) => m.pr && prKeyString(m.pr) === prKeyString(pr.key))
		});
	}
	for (const w of row.reviewWaits) {
		const who = requestee(w);
		lanes.push({
			key: `review:${w.request}`,
			label: `review on PR #${w.pr.number}${who ? `: ${who}` : ''}`,
			spans: [waitBar(row, w)],
			marks: []
		});
	}
	const own = row.marks.filter((m) => m.pr === undefined && announcedRun(row, m) === undefined);
	if (own.length > 0) lanes.push({ key: 'task', label: 'task', spans: [], marks: own });
	return lanes;
}

/** The runs of one agent profile. */
export interface ExecutorGroup {
	/** The agent profile; undefined for runs that name none. */
	executor?: string;
	/** One lane per run, labelled with its task. */
	lanes: Lane[];
}

/**
 * The rows' runs grouped by agent profile, in name order, the runs naming
 * none last; within a group, each run is its own lane, in start order.
 */
export function byExecutor(rows: readonly OverviewRow[]): ExecutorGroup[] {
	const groups = new Map<string | undefined, { run: Run; lane: Lane }[]>();
	for (const row of rows) {
		const numbers = attempts(row.runs);
		const title = row.title ?? rowName(row.root);
		for (const run of row.runs) {
			const n = numbers.get(run);
			const label = n === undefined ? title : `${title} · attempt ${n}`;
			const lane = runLane(row, run, `${label} · ${run.outcome}`, row);
			groups.set(run.agent, [...(groups.get(run.agent) ?? []), { run, lane }]);
		}
	}
	return [...groups.entries()]
		.sort(([a], [b]) => (a === undefined ? 1 : b === undefined ? -1 : a.localeCompare(b)))
		.map(([executor, runs]) => ({
			executor,
			lanes: [...runs].sort((a, b) => anchor(a.run) - anchor(b.run)).map((r) => r.lane)
		}));
}

/** A root's short name: "task 201", "page 57 (en)". */
export function rowName(root: Root): string {
	return root.task ? `task ${root.task.id}` : `page ${root.page.id} (${root.page.locale})`;
}
