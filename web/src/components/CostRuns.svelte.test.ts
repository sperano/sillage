import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { costPath } from '../lib/api';
import { parseTaskCost } from '../lib/cost/decode';
import type { LinkConfig } from '../lib/links';
import type { Root } from '../lib/model';
import { goldenResponse } from '../lib/testing/apiResponses';
import CostRuns from './CostRuns.svelte';

const MIXED: Root = { task: { instance: 'vikunja.example.com', id: 301 } };
const UNKNOWN: Root = { task: { instance: 'vikunja.example.com', id: 302 } };
const CONSOLES: LinkConfig = { kelosConsoles: { 'cluster-a': 'https://kelos.example.com' } };
const UID_PREFIX = '00000000-0000-4000-8000-0000000';

/** The runs of the golden cost of root, as the Go handler answers it. */
function show(root: Root, links: LinkConfig = CONSOLES) {
	const { runs } = parseTaskCost(goldenResponse(costPath(root)));
	return render(CostRuns, { runs, links });
}

/** The run of Task UID ending in suffix. */
function run(suffix: string) {
	const el = document.querySelector(`[data-run="cluster-a/agents/uid/${UID_PREFIX}${suffix}"]`);
	if (!el) throw new Error(`no run ${suffix}`);
	return page.elementLocator(el);
}

test("a complete run's estimate, its attempts, models and subagent threads", async () => {
	await show(MIXED);
	const retried = run('301ab');
	await expect
		.element(retried.getByRole('heading', { level: 4 }))
		.toHaveTextContent('implementation: vikunja-widgets-agent-opus-301');
	await expect
		.element(retried.getByTestId('run-estimate'))
		.toHaveTextContent(/^US\$13\.5464 \(complete\)$/);
	await expect.element(retried.getByTestId('run-reported')).toHaveTextContent(/^US\$12\.16$/);
	const items = (id: string) =>
		retried
			.getByTestId(id)
			.getByRole('listitem')
			.elements()
			.map((li) => li.textContent?.replace(/\s+/g, ' ').trim());
	expect(items('run-attempts')).toEqual([
		'pod vikunja-widgets-agent-opus-301-a1b2c: US$12.7064',
		'pod vikunja-widgets-agent-opus-301-r2r2r: US$0.84'
	]);
	expect(items('run-models')).toEqual([
		'claude-haiku-5-5 (Claude Code, standard): US$0.1064',
		'claude-opus-5-5 (Claude Code, fast): US$0.84',
		'claude-opus-5-5 (Claude Code, standard): US$12.30',
		'claude-sonnet-5 (Claude Code, standard): US$0.30'
	]);
	expect(items('run-threads')).toEqual([
		'session-a1, main loop: US$12.30',
		'session-a1, subagent agent-explore-1: US$0.1064',
		'session-a1, subagent agent-review-2, started by agent-explore-1: US$0.30',
		'session-a2, main loop: US$0.84'
	]);
});

test('each run links to its Task in the Kelos console', async () => {
	await show(MIXED);
	await expect
		.element(run('301aa').getByRole('link', { name: /Kelos console/ }))
		.toHaveAttribute(
			'href',
			'https://kelos.example.com/api/resources/tasks/agents/vikunja-widgets-agent-opus-301/logs'
		);
	await expect
		.element(run('301ac').getByRole('link', { name: /Kelos console/ }))
		.toHaveAttribute(
			'href',
			'https://kelos.example.com/api/resources/tasks/agents/pr-fix-widgets-codex-77/logs'
		);
});

test('without a console for the cluster, a run says why it has no link', async () => {
	await show(MIXED, {});
	await expect
		.element(run('301aa'))
		.toHaveTextContent(/none: no console is configured for cluster cluster-a/);
	expect(run('301aa').getByRole('link').elements()).toHaveLength(0);
});

test('the automation run is none, and lists no attempts', async () => {
	await show(MIXED);
	const automation = run('301ad');
	await expect
		.element(automation.getByTestId('run-estimate'))
		.toHaveTextContent(/^none: no model used \(no usage expected\)$/);
	await expect.element(automation.getByTestId('run-reported')).toHaveTextContent(/^none reported$/);
	expect(automation.getByTestId('run-attempts').elements()).toHaveLength(0);
});

test("a partial run is a lower bound, the runner's own cost apart", async () => {
	await show(MIXED);
	const codex = run('301ac');
	await expect
		.element(codex.getByTestId('run-estimate'))
		.toHaveTextContent(/^at least US\$4\.34 \(partial: a lower bound\)$/);
	await expect.element(codex.getByTestId('run-reported')).toHaveTextContent(/^US\$2\.50$/);
});

test('an unknown run is unknown, never US$0, with its reasons', async () => {
	await show(UNKNOWN);
	const early = run('302aa');
	await expect
		.element(early.getByTestId('run-estimate'))
		.toHaveTextContent(/^unknown \(unknown: nothing could be priced\)$/);
	expect(
		early
			.getByTestId('cost-notes')
			.getByRole('listitem')
			.elements()
			.map((li) => li.textContent)
	).toEqual([
		"It started before the runners' usage telemetry did.",
		'No usage events were read of it.'
	]);
	await expect.element(run('302ac')).toHaveTextContent(/not yet: more usage may still arrive/);
	await expect.element(run('302ac')).toHaveTextContent(/running at the last read/);
});
