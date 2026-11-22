import { readFileSync } from 'node:fs';
import { describe, expect, test } from 'vitest';
import { costPath } from '../api';
import { goldenResponse } from '../testing/apiResponses';
import { parseTaskCost } from './decode';
import { COST_NOTES } from './model';

const MIXED = { task: { instance: 'vikunja.example.com', id: 301 } };
const UNKNOWN = { task: { instance: 'vikunja.example.com', id: 302 } };

type Path = readonly (string | number)[];
type Node = Record<string | number, unknown>;

/** The golden cost of root, as the Go handler answers it. */
function golden(root: typeof MIXED): unknown {
	return goldenResponse(costPath(root));
}

/** Marks a field changed removes. */
const REMOVE = Symbol('remove');

/** The golden cost of root with the value at path set to to, or removed. */
function changed(root: typeof MIXED, path: Path, to: unknown): unknown {
	const body = golden(root);
	let parent = body as Node;
	for (const step of path.slice(0, -1)) parent = parent[step] as Node;
	const last = path.at(-1) as string | number;
	if (to === REMOVE) delete parent[last];
	else parent[last] = to;
	return body;
}

describe('the Go responses', () => {
	test('a task of mixed models decodes, its views partitioning the total', () => {
		const cost = parseTaskCost(golden(MIXED));
		expect(cost.prices.version).toBe('2026-11-22.1');
		expect(cost.total).toMatchObject({
			status: 'partial',
			estimatedUSD: '18.886400',
			reportedUSD: '15.660000',
			notes: ['cache-write-ttl-unknown', 'failed-run-may-lose-unsent-events']
		});
		expect(cost.byRole.map((r) => [r.role, r.runs, r.status])).toEqual([
			['automation', 1, 'none'],
			['implementation', 2, 'partial'],
			['pr-follow-up', 1, 'partial']
		]);
		const automation = cost.runs.find((r) => r.role === 'automation');
		expect(automation).toMatchObject({ status: 'none', notes: [], attempts: [], models: [] });
		expect(automation?.estimatedUSD).toBeUndefined();
		expect(cost.unattributed).toBeUndefined();
	});

	test('a task of unknown prices decodes, with no estimate where nothing was priced', () => {
		const cost = parseTaskCost(golden(UNKNOWN));
		expect(cost.final).toBe(false);
		expect(cost.unattributed).toMatchObject({ status: 'complete', estimatedUSD: '0.000150' });
		const sol = cost.byModel.find((m) => m.model === 'gpt-5.6-sol');
		// Go writes the nil list of price ids as null: no entry priced it.
		expect(sol).toMatchObject({ status: 'unknown', prices: [] });
		expect(sol?.estimatedUSD).toBeUndefined();
		expect(cost.runs.map((r) => [r.outcome, r.final, r.status])).toEqual([
			['succeeded', true, 'unknown'],
			['succeeded', false, 'unknown'],
			['running', false, 'unknown']
		]);
	});
});

describe('what Go never writes fails', () => {
	const ESTIMATE_RULE = 'an estimate is there exactly when the status is complete or partial';

	test.each<[string, typeof MIXED, Path, unknown, string]>([
		[
			'an unknown field',
			MIXED,
			['runs', 0, 'threads', 0, 'cost'],
			'1.00',
			'$.runs[0].threads[0]: unexpected field cost'
		],
		[
			'an unknown status',
			MIXED,
			['total', 'status'],
			'estimated',
			'$.total.status: unknown value "estimated"'
		],
		[
			'an unknown note code',
			MIXED,
			['byModel', 4, 'notes'],
			['cache-write-ttl-unknown', 'rate-limited'],
			'$.byModel[4].notes[1]: unknown value "rate-limited"'
		],
		[
			'an amount that is a number, not a string',
			MIXED,
			['runs', 1, 'reportedUSD'],
			12.16,
			'$.runs[1].reportedUSD: expected a string, got a number'
		],
		[
			'an estimate of an unknown breakdown',
			UNKNOWN,
			['runs', 0, 'estimatedUSD'],
			'0.000000',
			`$.runs[0]: ${ESTIMATE_RULE}`
		],
		[
			'no estimate of a partial breakdown',
			MIXED,
			['total', 'estimatedUSD'],
			REMOVE,
			`$.total: ${ESTIMATE_RULE}`
		],
		[
			'categories out of order',
			MIXED,
			['total', 'categories', 0, 'category'],
			'output',
			'$.total.categories: expected the categories'
		],
		[
			'a note twice',
			UNKNOWN,
			['total', 'notes'],
			['in-progress', 'in-progress'],
			'$.total.notes[1]: duplicate record in-progress'
		],
		['a null list of runs', MIXED, ['runs'], null, '$.runs: expected an array, got null']
	])('%s', (_, root, path, to, message) => {
		expect(() => parseTaskCost(changed(root, path, to))).toThrow(message);
	});

	test.each([['1e3'], ['-1.00'], ['01.5'], ['$4.34'], ['']])('the amount %j', (amount) => {
		expect(() => parseTaskCost(changed(MIXED, ['total', 'estimatedUSD'], amount))).toThrow(
			`$.total.estimatedUSD: ${JSON.stringify(amount)} is not a non-negative decimal`
		);
	});
});

test("the notes are internal/cost's Note constants, in their order", () => {
	const go = readFileSync(
		new URL('../../../../internal/cost/breakdown.go', import.meta.url),
		'utf8'
	);
	const block = /The notes of a breakdown[^]*?\nconst \(([^]*?)\n\)/.exec(go)?.[1] ?? '';
	const codes = [...block.matchAll(/^\s*Note\w+\s*=\s*"([^"]+)"/gm)].map((m) => m[1]);
	expect(codes).toEqual(COST_NOTES);
});
