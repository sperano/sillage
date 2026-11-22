// The runtime check of a task's cost as internal/api writes it
// (GET /api/v1/cost/task/...): every field and value the Go cost package
// can write and no other, so that a code the page has no words for, or a
// status it would render wrong, fails here instead.

import {
	type Decoder,
	anyString,
	array,
	bool,
	decode,
	fail,
	nonNegativeInteger,
	object,
	oneOf,
	positiveInteger,
	refine,
	text,
	timestamp,
	unique
} from '../check';
import { decimal, root, runKey } from '../decodeModel';
import { OUTCOMES, RUN_ROLES } from '../model';
import {
	type AttemptCost,
	type Breakdown,
	type CategoryCost,
	type ModelCost,
	type RoleCost,
	type RunCost,
	type TaskCost,
	type ThreadCost,
	CATEGORIES,
	COST_NOTES,
	COST_STATUSES,
	ESTIMATED_STATUSES,
	PROVIDERS,
	RUNNERS,
	TIERS
} from './model';

const categoryCost: Decoder<CategoryCost> = object(
	{ category: oneOf(CATEGORIES), tokens: nonNegativeInteger },
	{ estimatedUSD: decimal, unpriced: bool }
);

// Go writes every category, in its order, in every breakdown.
const categories: Decoder<CategoryCost[]> = refine(array(categoryCost), (list, path) => {
	if (list.map((c) => c.category).join() !== CATEGORIES.join()) {
		fail(path, `expected the categories ${CATEGORIES.join(', ')}, in that order`);
	}
});

const notes = unique(array(oneOf(COST_NOTES)), (n) => n);

/** The fields of a Breakdown, which Go embeds in each of its views. */
const breakdownRequired = {
	status: oneOf(COST_STATUSES),
	requests: nonNegativeInteger,
	failedRequests: nonNegativeInteger,
	categories
};
const breakdownOptional = { estimatedUSD: decimal, reportedUSD: decimal, notes };

type Decoded<B> = Omit<B, 'notes'> & { notes?: Breakdown['notes'] };

// Go prices something exactly when the status is complete or partial: an
// estimate is never missing from those, nor present (as a zero) in others.
// Notes Go omitted are none.
function withEstimate<B extends Breakdown>(decoder: Decoder<Decoded<B>>): Decoder<B> {
	return (value, path) => {
		const b = decoder(value, path);
		const estimated = ESTIMATED_STATUSES.includes(b.status);
		if ((b.estimatedUSD !== undefined) !== estimated) {
			fail(
				path,
				`an estimate is there exactly when the status is ${ESTIMATED_STATUSES.join(' or ')}`
			);
		}
		// b holds every field of B but notes, defaulted here.
		return { ...b, notes: b.notes ?? [] } as B;
	};
}

const breakdown = withEstimate<Breakdown>(object(breakdownRequired, breakdownOptional));

const attemptCost = withEstimate<AttemptCost>(
	object({ ...breakdownRequired, pod: text }, breakdownOptional)
);

// A nil list of price ids is written as null: no entry priced the model.
const priceIds: Decoder<string[]> = (value, path) =>
	value === null ? [] : array(text)(value, path);

const modelCost = withEstimate<ModelCost>(
	object(
		{ ...breakdownRequired, runner: oneOf(RUNNERS), model: anyString, prices: priceIds },
		{ ...breakdownOptional, tier: oneOf(TIERS) }
	)
);

const threadCost = withEstimate<ThreadCost>(
	object(
		{ ...breakdownRequired, runner: oneOf(RUNNERS), stream: text },
		{ ...breakdownOptional, agent: text, parentAgent: text }
	)
);

const roleCost = withEstimate<RoleCost>(
	object({ ...breakdownRequired, role: oneOf(RUN_ROLES), runs: positiveInteger }, breakdownOptional)
);

const runCost = withEstimate<RunCost>(
	object(
		{
			...breakdownRequired,
			key: runKey,
			role: oneOf(RUN_ROLES),
			outcome: oneOf(OUTCOMES),
			final: bool,
			attempts: array(attemptCost),
			models: array(modelCost),
			threads: array(threadCost)
		},
		{ ...breakdownOptional, taskName: text, model: text, ended: timestamp }
	)
);

const taskCost: Decoder<TaskCost> = object(
	{
		root,
		prices: object({
			version: text,
			verifiedAt: text,
			sources: array(object({ provider: oneOf(PROVIDERS), url: text, readAt: text }))
		}),
		assumptions: array(object({ code: text, text })),
		feed: object({ on: bool }, { since: timestamp, readThrough: timestamp }),
		final: bool,
		total: breakdown,
		byRole: unique(array(roleCost), (r) => r.role),
		byModel: array(modelCost),
		runs: array(runCost)
	},
	{ unattributed: breakdown }
);

/** Decodes the body of a task's cost, throwing a DecodeError on anything Go never writes. */
export function parseTaskCost(body: unknown): TaskCost {
	return decode(taskCost, body);
}
