// What the overview says of an instant: which runs were executing, which
// review requests were waiting, and where nothing is known. Only records
// answer: an instant no record covers is unknown, never idle.

import { boundTime, coverageKey, type Coverage, type Run, type Source } from '../model';
import type { OverviewRow } from './model';

/** What a run's records say of it at an instant. */
export type RunState = 'running' | 'waiting' | 'not-running' | 'unknown';

/** The last successful read of a source instance, by the row's coverage. */
export function lastRead(
	coverage: readonly Coverage[],
	source: Source,
	instance: string
): number | undefined {
	const c = coverage.find(
		(c) => coverageKey(c.source, c.instance) === coverageKey(source, instance)
	);
	return c?.lastSuccessfulRead === undefined ? undefined : Date.parse(c.lastSuccessfulRead);
}

/** The last read of the run's Kelos cluster: an open run is known to continue until then. */
export function runLastRead(row: OverviewRow, run: Run): number | undefined {
	return lastRead(row.coverage, 'kelos', run.key.cluster);
}

/**
 * The run's state at t by its own records: its creation, start and end,
 * and for a run not ended, the last read that saw it so. Whatever they do
 * not cover is unknown.
 */
export function runStateAt(run: Run, t: number, read: number | undefined): RunState {
	const created = run.created === undefined ? undefined : Date.parse(run.created);
	const start = boundTime(run.interval.start);
	const end = boundTime(run.interval.end);
	if (end !== undefined && t >= end) return 'not-running';
	if (created !== undefined && t < created) return 'not-running';
	const queued = run.outcome === 'waiting';
	if (start !== undefined && t < start) return created === undefined ? 'not-running' : 'waiting';
	if (start === undefined && !(queued && created !== undefined)) return 'unknown';
	// Started (or queued since its creation), and not ended before t.
	if (end !== undefined) return queued ? 'waiting' : 'running';
	if (run.interval.end.state !== 'open' || read === undefined || t > read) return 'unknown';
	return queued ? 'waiting' : 'running';
}

/** Whether the coverage says the source's records at t are known. */
export function knownAt(c: Coverage, t: number): boolean {
	if (c.lastSuccessfulRead === undefined || t > Date.parse(c.lastSuccessfulRead)) return false;
	return !(c.gaps ?? []).some((g) => t >= Date.parse(g.start) && t < Date.parse(g.end));
}

/**
 * Whether the row's Kelos records are unknown at t: a Kelos coverage of it
 * is, or it has runs and no Kelos coverage at all. Then a run of the task
 * may be missing from the row.
 */
export function kelosUnknownAt(row: OverviewRow, t: number): boolean {
	const kelos = row.coverage.filter((c) => c.source === 'kelos');
	if (kelos.length === 0) return row.runs.length > 0;
	return kelos.some((c) => !knownAt(c, t));
}

/** One row's runs executing at an instant. */
export interface Executing {
	row: OverviewRow;
	runs: Run[];
}

/** What the rows say of an instant. */
export interface Instant {
	/** The rows with a run executing at t, and those runs. */
	executing: Executing[];
	/** The rows with no run executing at t whose runs at t are unknown. */
	unknown: OverviewRow[];
}

/** Which rows had a run executing at t, and which are unknown then. */
export function instantOf(rows: readonly OverviewRow[], t: number): Instant {
	const out: Instant = { executing: [], unknown: [] };
	for (const row of rows) {
		const states = row.runs.map((r) => ({ run: r, state: runStateAt(r, t, runLastRead(row, r)) }));
		const running = states.filter((s) => s.state === 'running').map((s) => s.run);
		if (running.length > 0) out.executing.push({ row, runs: running });
		else if (states.some((s) => s.state === 'unknown') || kelosUnknownAt(row, t)) {
			out.unknown.push(row);
		}
	}
	return out;
}
