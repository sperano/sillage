import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { HttpError, costPath } from '../lib/api';
import { parseTaskCost } from '../lib/cost/decode';
import type { TaskCost } from '../lib/cost/model';
import type { Load } from '../lib/load.svelte';
import type { Root } from '../lib/model';
import { goldenResponse } from '../lib/testing/apiResponses';
import CostPanel from './CostPanel.svelte';

const MIXED: Root = { task: { instance: 'vikunja.example.com', id: 301 } };
const UNKNOWN: Root = { task: { instance: 'vikunja.example.com', id: 302 } };

/** The panel showing cost. */
function show(cost: Load<TaskCost>) {
	return render(CostPanel, { cost, links: {}, timeZone: 'UTC' });
}

/** The panel showing the golden cost of root, as the Go handler answers it. */
function showGolden(root: Root) {
	return show({ state: 'loaded', value: parseTaskCost(goldenResponse(costPath(root))) });
}

const section = () => page.getByRole('region', { name: 'Estimated cost' });

test('the section has the stable anchor id cost', async () => {
	await showGolden(MIXED);
	await expect.element(section()).toHaveAttribute('id', 'cost');
});

test('a partial total is shown as a lower bound, the reported cost apart', async () => {
	await showGolden(MIXED);
	await expect.element(page.getByTestId('cost-total')).toHaveTextContent(/^at least US\$18\.8864$/);
	await expect
		.element(page.getByTestId('cost-status'))
		.toHaveTextContent('(partial: a lower bound)');
	await expect
		.element(page.getByTestId('cost-reported'))
		.toHaveTextContent(
			/^US\$15\.66 \(their own estimate, shown apart: never added to the one above\)$/
		);
	await expect
		.element(page.getByTestId('cost-final'))
		.toHaveTextContent(/^yes: every run ended and its usage was read$/);
	await expect
		.element(page.getByTestId('cost-requests'))
		.toHaveTextContent(/^9 requests, 1 of them failed$/);
	const notes = page.getByTestId('cost-total-notes').getByRole('listitem');
	expect(notes.elements().map((n) => n.textContent)).toEqual([
		'Some cache writes did not report how long they are kept, so they are not priced.',
		'The run failed: usage events it had not sent yet may be missing.'
	]);
});

test('the price table version and date, its sources and the assumptions are shown', async () => {
	await showGolden(MIXED);
	await expect
		.element(page.getByTestId('cost-prices'))
		.toHaveTextContent(/^version 2026-11-22\.1, verified 2026-11-22$/);
	await expect
		.element(section().getByRole('link', { name: 'openai' }))
		.toHaveAttribute('href', 'https://developers.openai.com/api/docs/pricing');
	await expect
		.element(page.getByTestId('cost-assumptions').getByRole('listitem').first())
		.toHaveTextContent(
			"An estimate of what the same usage would cost at the providers' public API list prices. It is not what was billed, nor money saved."
		);
	expect(page.getByTestId('cost-assumptions').getByRole('listitem').elements()).toHaveLength(7);
});

test('by role, the automation run is none, never US$0', async () => {
	await showGolden(MIXED);
	const cells = (role: string) =>
		[
			...page.getByTestId('cost-by-role').element().querySelectorAll(`[data-role="${role}"] > *`)
		].map((c) => c.textContent);
	expect(cells('automation')).toEqual([
		'automation',
		'1',
		'none: no model used',
		'no usage expected',
		'none reported'
	]);
	expect(cells('implementation')).toEqual([
		'implementation',
		'2',
		'at least US$14.5464',
		'partial: a lower bound',
		'US$13.16'
	]);
});

test('by model and by category, unpriced usage is unknown or a lower bound', async () => {
	await showGolden(UNKNOWN);
	const rows = (id: string) =>
		[...page.getByTestId(id).element().querySelectorAll('tbody tr')].map((r) =>
			[...r.children].map((c) => c.textContent)
		);
	expect(rows('cost-by-model')).toEqual([
		[
			'claude-haiku-5',
			'Claude Code',
			'standard',
			'2',
			'unknown',
			'unknown: nothing could be priced',
			'US$0.001'
		],
		['claude-haiku-5-5', 'Claude Code', 'standard', '1', 'US$0.00015', 'complete', 'none reported'],
		[
			'gpt-5.6-sol',
			'Codex',
			'standard',
			'1',
			'unknown',
			'unknown: nothing could be priced',
			'none reported'
		]
	]);
	expect(rows('cost-categories')).toEqual([
		['uncached input', '4000', 'at least US$0.0001'],
		['cache reads', '0', 'none'],
		['cache writes (5 minutes)', '0', 'none'],
		['cache writes (1 hour)', '0', 'none'],
		['cache writes (lifetime unknown)', '500', 'unknown'],
		['output', '400', 'at least US$0.00005']
	]);
});

test('a cost that is not final says more usage may arrive, and counts what no run was joined to', async () => {
	await showGolden(UNKNOWN);
	await expect
		.element(page.getByTestId('cost-final'))
		.toHaveTextContent(/^not yet: more usage may still arrive$/);
	await expect.element(page.getByTestId('cost-total')).toHaveTextContent(/^at least US\$0\.00015$/);
	await expect
		.element(page.getByTestId('cost-unattributed'))
		.toHaveTextContent(/^US\$0\.00015, counted in the total$/);
	await expect
		.element(page.getByTestId('cost-feed'))
		.toHaveTextContent(
			/^read through 2026-11-24 09:40:00 UTC \(the runners' telemetry started 2026-11-22 18:00:00 UTC\)$/
		);
});

test('a failed request is an error of the section alone', async () => {
	await show({
		state: 'failed',
		error: new HttpError('GET /x', 503, { title: 'Service Unavailable' })
	});
	await expect
		.element(section().getByTestId('cost-failed'))
		.toHaveTextContent(
			"Sillage's server answered with an error (503 Service Unavailable). The estimated cost is not shown."
		);
	expect(page.getByTestId('cost-total').elements()).toHaveLength(0);
});

test('while loading, the section says so', async () => {
	await show({ state: 'loading' });
	await expect
		.element(section().getByRole('status'))
		.toHaveTextContent('Loading the estimated cost…');
});
