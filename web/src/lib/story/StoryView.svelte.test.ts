import { describe, expect, test } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { createRawSnippet } from 'svelte';
import StoryView from './StoryView.svelte';
import { Selection } from '../selection.svelte';
import { goldenStory } from '../testing/stories';
import type { Selection as SelectionType } from '../selection.svelte';

const rows = () => page.getByRole('region', { name: 'Events' }).getByRole('button');

describe('announced-run-only', () => {
	test('the announced-only run shows no duration', async () => {
		render(StoryView, { story: goldenStory('announced-run-only') });
		await page.getByRole('button', { name: /Run vikunja-widgets-agent-opus-105/ }).click();
		const duration = page.getByTestId('duration');
		await expect
			.element(duration)
			.toHaveTextContent('none: the start is unknown and the end is unknown');
		expect(duration.element().textContent).not.toMatch(/[0-9]/);
		await expect.element(page.getByTestId('start')).toHaveTextContent('unknown');
	});

	test('a finished run shows its duration', async () => {
		render(StoryView, { story: goldenStory('failed-run-retry') });
		await page.getByRole('button', { name: /Run vikunja-widgets-agent-opus-104.*failed/ }).click();
		await expect.element(page.getByTestId('duration')).toHaveTextContent('6 min 37 s');
	});
});

describe('wiki-rooted-request', () => {
	const live = 'https://wiki.example.com/en/widgets/design/storage-layout';

	test('the live edit links the live page, in the list and in the detail', async () => {
		render(StoryView, { story: goldenStory('wiki-rooted-request') });
		const row = page.getByRole('listitem').filter({ hasText: 'Page edited (live version)' });
		await expect.element(row.getByRole('link')).toHaveAttribute('href', live);
		await row.getByRole('button').click();
		const detail = page.getByRole('region', { name: 'Detail' });
		await expect
			.element(detail.getByRole('link', { name: 'wiki page, live version' }))
			.toHaveAttribute('href', live);
	});

	test('the ambiguous live edit lists both candidate runs and selects one', async () => {
		render(StoryView, { story: goldenStory('wiki-rooted-request') });
		await page.getByRole('button', { name: /Page edited \(live version\)/ }).click();
		const detail = page.getByRole('region', { name: 'Detail' });
		await expect
			.element(
				detail.getByText(/Ambiguous: 2 candidates fit the evidence, none was chosen/).first()
			)
			.toBeVisible();
		await expect.element(detail.getByText(/confidence: heuristic/).first()).toBeVisible();
		await detail
			.getByRole('button', { name: 'Run vikunja-widgets-agent-opus-103' })
			.first()
			.click();
		await expect
			.element(detail.getByRole('heading', { level: 3 }))
			.toHaveTextContent('Run vikunja-widgets-agent-opus-103 (implementation): succeeded');
	});
});

describe('keyboard', () => {
	test('arrows, Home and End move the selection; Tab reaches one row', async () => {
		render(StoryView, { story: goldenStory('implementation-merged') });
		const buttons = rows();
		const count = buttons.elements().length;
		expect(buttons.elements().filter((b) => b.tabIndex === 0)).toHaveLength(1);

		await userEvent.tab();
		await expect.element(buttons.nth(0)).toHaveFocus();
		await userEvent.keyboard('{ArrowDown}');
		await expect.element(buttons.nth(1)).toHaveAttribute('aria-current', 'true');
		await expect.element(buttons.nth(1)).toHaveFocus();
		const heading = page.getByRole('region', { name: 'Detail' }).getByRole('heading', { level: 3 });
		await expect.element(heading).toHaveTextContent('Seen in bucket Doing');

		await userEvent.keyboard('{End}');
		await expect.element(buttons.nth(count - 1)).toHaveAttribute('aria-current', 'true');
		await userEvent.keyboard('{ArrowDown}');
		await expect.element(buttons.nth(count - 1)).toHaveAttribute('aria-current', 'true');
		await userEvent.keyboard('{Home}');
		await expect.element(buttons.nth(0)).toHaveAttribute('aria-current', 'true');
		await expect.element(heading).toHaveTextContent('Task created');
		await userEvent.keyboard('{ArrowUp}');
		await expect.element(buttons.nth(0)).toHaveAttribute('aria-current', 'true');
		expect(buttons.elements().filter((b) => b.tabIndex === 0)).toEqual([buttons.nth(0).element()]);
	});

	test('a selection made elsewhere, as the graph does, selects in the list and the detail', async () => {
		const story = goldenStory('failed-run-retry');
		const selection = new Selection();
		render(StoryView, { story, selection });
		selection.select({
			kind: 'run',
			id: 'cluster-a/agents/uid/00000000-0000-4000-8000-00000000104b'
		});
		const row = page.getByRole('button', { name: /Run vikunja-widgets-agent-opus-104.*succeeded/ });
		await expect.element(row).toHaveAttribute('aria-current', 'true');
		await expect.element(row).toHaveAttribute('tabindex', '0');
		await expect
			.element(page.getByRole('region', { name: 'Detail' }).getByRole('heading', { level: 3 }))
			.toHaveTextContent(/succeeded/);
	});
});

describe('layout', () => {
	const graph = createRawSnippet((selection: () => SelectionType) => ({
		render: () =>
			`<div data-testid="graph" style="height: 200px" data-selected="${selection().current?.id ?? ''}">graph</div>`
	}));

	function boxes() {
		const g = page.getByTestId('graph').element().getBoundingClientRect();
		const list = page.getByRole('region', { name: 'Events' }).element().getBoundingClientRect();
		const detail = page.getByRole('region', { name: 'Detail' }).element().getBoundingClientRect();
		return { g, list, detail };
	}

	test('narrow screens stack the panels under the graph', async () => {
		await page.viewport(400, 900);
		render(StoryView, { story: goldenStory('implementation-merged'), graph });
		const { g, list, detail } = boxes();
		expect(list.top).toBeGreaterThanOrEqual(g.bottom);
		expect(detail.top).toBeGreaterThanOrEqual(list.bottom);
	});

	test('wide screens put the panels beside the graph', async () => {
		await page.viewport(1400, 900);
		render(StoryView, { story: goldenStory('implementation-merged'), graph });
		const { g, list, detail } = boxes();
		expect(list.left).toBeGreaterThanOrEqual(g.right);
		expect(detail.left).toBeGreaterThanOrEqual(g.right);
	});

	test('coverage and the next action come before the graph', async () => {
		render(StoryView, { story: goldenStory('implementation-merged'), graph });
		const g = page.getByTestId('graph').element().getBoundingClientRect();
		for (const name of ['Coverage', 'Next action']) {
			const box = page.getByRole('region', { name }).element().getBoundingClientRect();
			expect(box.bottom).toBeLessThanOrEqual(g.top);
		}
	});
});
