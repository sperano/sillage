// A task's estimated API-equivalent cost (Go's cost.TaskCost, from
// internal/cost): what its agents' usage would have cost at the providers'
// public API list prices. Not what was billed, and not money saved.

import type { Outcome, Root, RunKey, RunRole } from '../model';

/** The id of a story page's cost section: /task/<instance>/<id>#cost links to it. */
export const COST_ANCHOR = 'cost';

/** How much of a breakdown's usage is known and priced (Go's cost.Status). */
export const COST_STATUSES = ['complete', 'partial', 'unknown', 'none'] as const;
export type CostStatus = (typeof COST_STATUSES)[number];

/** The statuses whose breakdown has an estimate: partial's is a lower bound. */
export const ESTIMATED_STATUSES: readonly CostStatus[] = ['complete', 'partial'];

/** The ways tokens are priced, in the order a breakdown lists them (Go's cost.Category). */
export const CATEGORIES = [
	'uncachedInput',
	'cacheRead',
	'cacheWrite5m',
	'cacheWrite1h',
	'cacheWriteUnknownTTL',
	'output'
] as const;
export type Category = (typeof CATEGORIES)[number];

/** The notes of a breakdown (Go's cost.Note* constants): keep in sync with internal/cost. */
export const COST_NOTES = [
	'no-usage-events',
	'before-telemetry',
	'usage-feed-off',
	'in-progress',
	'not-final',
	'price-unknown',
	'tier-unknown',
	'model-not-reported',
	'cache-write-ttl-unknown',
	'tokens-not-reported',
	'failed-run-may-lose-unsent-events',
	'unattributed-usage',
	'attributed-by-task-name'
] as const;
export type CostNote = (typeof COST_NOTES)[number];

/** The runners whose usage sillage reads (Go's model.Runner). */
export const RUNNERS = ['claude-code', 'codex'] as const;
export type Runner = (typeof RUNNERS)[number];

/** The tiers the price table covers (Go's cost.Tier). */
export const TIERS = ['standard', 'fast', 'flex'] as const;
export type Tier = (typeof TIERS)[number];

/** The providers whose prices the table holds (Go's cost.Provider). */
export const PROVIDERS = ['anthropic', 'openai'] as const;
export type Provider = (typeof PROVIDERS)[number];

/** One category of a breakdown. */
export interface CategoryCost {
	category: Category;
	tokens: number;
	/** The priced part, a decimal string; absent when none was priced. */
	estimatedUSD?: string;
	/** Some of the category's tokens have no known price, or were not reported. */
	unpriced?: boolean;
}

/** The estimated cost of some usage (Go's cost.Breakdown). */
export interface Breakdown {
	status: CostStatus;
	/**
	 * The priced part in US dollars, a decimal string: there exactly when the
	 * status is complete or partial, and a lower bound when partial.
	 */
	estimatedUSD?: string;
	/** The runners' own estimate: shown apart, never added to estimatedUSD. */
	reportedUSD?: string;
	requests: number;
	failedRequests: number;
	categories: CategoryCost[];
	/** The reasons for a status other than complete, and how usage was attributed; sorted. */
	notes: CostNote[];
}

/** The usage of one of a run's pods: a retried Job has one per attempt. */
export interface AttemptCost extends Breakdown {
	pod: string;
}

/** The usage of one model at one tier. */
export interface ModelCost extends Breakdown {
	runner: Runner;
	/** The model the runner reported; empty when not reported. */
	model: string;
	tier?: Tier;
	/** The ids of the price entries that priced it. */
	prices: string[];
}

/** The usage of one session or conversation, and within it of its main loop or one subagent. */
export interface ThreadCost extends Breakdown {
	runner: Runner;
	stream: string;
	/** The subagent; absent for the main loop. */
	agent?: string;
	parentAgent?: string;
}

/** The usage of the runs of one role. */
export interface RoleCost extends Breakdown {
	role: RunRole;
	runs: number;
}

/** One run's estimate. */
export interface RunCost extends Breakdown {
	key: RunKey;
	taskName?: string;
	role: RunRole;
	outcome: Outcome;
	/** The model the run was configured with; the ones it used are in models. */
	model?: string;
	ended?: string;
	/** The run ended and the feed read far enough past its end: no more usage is expected. */
	final: boolean;
	attempts: AttemptCost[];
	models: ModelCost[];
	threads: ThreadCost[];
}

/** Where a price table's prices were read. */
export interface PriceSource {
	provider: Provider;
	url: string;
	readAt: string;
}

/** The price table an estimate used. */
export interface PricesRef {
	version: string;
	verifiedAt: string;
	sources: PriceSource[];
}

/** One stated assumption of every estimate. */
export interface Assumption {
	code: string;
	text: string;
}

/** What the usage feed has read. */
export interface Feed {
	on: boolean;
	/** When the runners' usage telemetry started; absent when unknown. */
	since?: string;
	/** The feed has read every event before it; absent when it never read. */
	readThrough?: string;
}

/** A task's estimated API-equivalent cost (Go's cost.TaskCost). */
export interface TaskCost {
	root: Root;
	prices: PricesRef;
	assumptions: Assumption[];
	feed: Feed;
	/** Every run ended and its usage was read: no more usage is expected. */
	final: boolean;
	total: Breakdown;
	/** With unattributed, adds up to total. */
	byRole: RoleCost[];
	byModel: ModelCost[];
	runs: RunCost[];
	/** The usage no single run could be joined to; counted in total. */
	unattributed?: Breakdown;
}
