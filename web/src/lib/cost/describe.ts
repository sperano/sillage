// Plain words for a task's estimated cost: an estimate is an estimate, a
// partial one a lower bound, and a missing one unknown, never zero.

import { named } from '../named';
import type { Breakdown, Category, CostNote, CostStatus, Runner, ThreadCost } from './model';

/** How many decimals an amount keeps at least, trailing zeros or not: cents. */
const MIN_DECIMALS = 2;

/**
 * A decimal string of US dollars as written to a reader: exact, with its
 * trailing zeros dropped down to cents ("18.886400" → "US$18.8864",
 * "4.340000" → "US$4.34", "0.000150" → "US$0.00015"). Rounding would
 * show a lower bound above itself, or a small cost as nothing.
 */
export function usd(amount: string): string {
	const [whole, fraction = ''] = amount.split('.');
	const trimmed = fraction.replace(/0+$/, '').padEnd(MIN_DECIMALS, '0');
	return `US$${whole}.${trimmed}`;
}

/**
 * A breakdown's estimate: the amount when complete, "at least" it when
 * partial, and words when there is none.
 */
export function estimateText(b: Pick<Breakdown, 'status' | 'estimatedUSD'>): string {
	switch (b.status) {
		case 'complete':
			return b.estimatedUSD === undefined ? 'unknown' : usd(b.estimatedUSD);
		case 'partial':
			return b.estimatedUSD === undefined ? 'unknown' : `at least ${usd(b.estimatedUSD)}`;
		case 'unknown':
			return 'unknown';
		case 'none':
			return 'none: no model used';
	}
}

/** The runners' own estimate, or that none reported one. */
export function reportedText(b: Pick<Breakdown, 'reportedUSD'>): string {
	return b.reportedUSD === undefined ? 'none reported' : usd(b.reportedUSD);
}

const statusLabels: Record<CostStatus, string> = {
	complete: 'complete',
	partial: 'partial: a lower bound',
	unknown: 'unknown: nothing could be priced',
	none: 'no usage expected'
};

export function statusLabel(status: CostStatus): string {
	return named(statusLabels, status);
}

const noteTexts: Record<CostNote, string> = {
	'no-usage-events': 'No usage events were read of it.',
	'before-telemetry': "It started before the runners' usage telemetry did.",
	'usage-feed-off': "Sillage's usage feed is off: no usage is read.",
	'in-progress': 'It is still running.',
	'not-final': 'More usage may still arrive.',
	'price-unknown': 'Some usage is of a model or tier with no known price.',
	'tier-unknown': 'Some usage reported a tier the price table does not know.',
	'model-not-reported': 'Some usage did not report its model.',
	'cache-write-ttl-unknown':
		'Some cache writes did not report how long they are kept, so they are not priced.',
	'tokens-not-reported': 'Some usage did not report all its tokens.',
	'failed-run-may-lose-unsent-events':
		'The run failed: usage events it had not sent yet may be missing.',
	'unattributed-usage': 'Some usage could not be joined to a single run; the total counts it.',
	'attributed-by-task-name': 'Some usage was joined to its run by Task name and time, not by pod.'
};

/** A note in a sentence. */
export function noteText(note: CostNote): string {
	return named(noteTexts, note);
}

const categoryLabels: Record<Category, string> = {
	uncachedInput: 'uncached input',
	cacheRead: 'cache reads',
	cacheWrite5m: 'cache writes (5 minutes)',
	cacheWrite1h: 'cache writes (1 hour)',
	cacheWriteUnknownTTL: 'cache writes (lifetime unknown)',
	output: 'output'
};

export function categoryLabel(c: Category): string {
	return named(categoryLabels, c);
}

const runnerLabels: Record<Runner, string> = { 'claude-code': 'Claude Code', codex: 'Codex' };

export function runnerLabel(r: Runner): string {
	return named(runnerLabels, r);
}

/** A thread's name: its stream, and its subagent and the one that started it, if any. */
export function threadLabel(t: Pick<ThreadCost, 'stream' | 'agent' | 'parentAgent'>): string {
	if (t.agent === undefined) return `${t.stream}, main loop`;
	const parent = t.parentAgent === undefined ? '' : `, started by ${t.parentAgent}`;
	return `${t.stream}, subagent ${t.agent}${parent}`;
}
