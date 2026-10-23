import { afterEach, describe, expect, test } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import StoryView from './StoryView.svelte';
import StoryWithGraph from '../testing/StoryWithGraph.svelte';
import { Selection } from '../selection.svelte';
import { goldenStory } from '../testing/stories';

const NARROW = { width: 400, height: 900 };
const WIDE = { width: 1400, height: 900 };
/** Vitest's default browser viewport, restored after a test changes it. */
const DEFAULT_VIEWPORT = { width: 414, height: 896 };

const RETRY_RUN = 'cluster-a/agents/uid/00000000-0000-4000-8000-00000000104b';

const rows = () => page.getByRole('region', { name: 'Events' }).getByRole('button');

describe('announced-run-only', () => {
	test('the announced-only run shows no duration', async () => {
		render(StoryView, { story: goldenStory('announced-run-only') });
		await page.getByRole('button', { name: /Run vikunja-widgets-agent-opus-105/ }).click();
		const duration = page.getByTestId('duration');
		await expect
			.element(duration)
			.toHaveTextContent('none: the start is unknown and the end is unknown');
		await expect.element(duration).not.toHaveTextContent(/[0-9]/);
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
		const heading = detail.getByRole('heading', { level: 3 });
		await expect
			.element(heading)
			.toHaveTextContent('Run vikunja-widgets-agent-opus-103 (implementation): succeeded');
		// The button is gone with the detail it was in; the focus is not lost.
		await expect.element(heading).toHaveFocus();
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

		await userEvent.tab();
		const list = page.getByRole('region', { name: 'Events' }).element();
		expect(list.contains(document.activeElement)).toBe(false);
	});

	test('a key with a modifier is left to the browser', async () => {
		render(StoryView, { story: goldenStory('implementation-merged') });
		const buttons = rows();
		await buttons.nth(1).click();
		for (const key of [
			'{Shift>}{ArrowDown}{/Shift}',
			'{Alt>}{Home}{/Alt}',
			'{Control>}{End}{/Control}'
		]) {
			await userEvent.keyboard(key);
			await expect.element(buttons.nth(1)).toHaveAttribute('aria-current', 'true');
		}
	});

	test('a selection made elsewhere, as the graph does, selects in the list and the detail', async () => {
		const story = goldenStory('failed-run-retry');
		const selection = new Selection();
		render(StoryView, { story, selection });
		selection.select({ kind: 'run', id: RETRY_RUN });
		const row = page.getByRole('button', { name: /Run vikunja-widgets-agent-opus-104.*succeeded/ });
		await expect.element(row).toHaveAttribute('aria-current', 'true');
		await expect.element(row).toHaveAttribute('tabindex', '0');
		await expect
			.element(page.getByRole('region', { name: 'Detail' }).getByRole('heading', { level: 3 }))
			.toHaveTextContent(/succeeded/);
	});
});

describe('graph', () => {
	test('the graph and the list share the selection', async () => {
		render(StoryWithGraph, {
			story: goldenStory('failed-run-retry'),
			refs: [{ kind: 'run', id: RETRY_RUN }]
		});
		await page.getByRole('button', { name: `graph ${RETRY_RUN}` }).click();
		await expect
			.element(page.getByRole('button', { name: /Run vikunja-widgets-agent-opus-104.*succeeded/ }))
			.toHaveAttribute('aria-current', 'true');

		await page.getByRole('button', { name: /Task created/ }).click();
		await expect
			.element(page.getByTestId('graph-selected'))
			.toHaveTextContent('vikunja/vikunja.example.com/tasks/104');
	});
});

describe('layout', () => {
	afterEach(async () => {
		await page.viewport(DEFAULT_VIEWPORT.width, DEFAULT_VIEWPORT.height);
	});

	function boxes() {
		const g = page.getByTestId('graph').element().getBoundingClientRect();
		const list = page.getByRole('region', { name: 'Events' }).element().getBoundingClientRect();
		const detail = page.getByRole('region', { name: 'Detail' }).element().getBoundingClientRect();
		return { g, list, detail };
	}

	test('narrow screens stack the panels under the graph', async () => {
		await page.viewport(NARROW.width, NARROW.height);
		render(StoryWithGraph, { story: goldenStory('implementation-merged') });
		const { g, list, detail } = boxes();
		expect(list.top).toBeGreaterThanOrEqual(g.bottom);
		expect(detail.top).toBeGreaterThanOrEqual(list.bottom);
	});

	test('wide screens put the panels beside the graph', async () => {
		await page.viewport(WIDE.width, WIDE.height);
		render(StoryWithGraph, { story: goldenStory('implementation-merged') });
		const { g, list, detail } = boxes();
		expect(list.left).toBeGreaterThanOrEqual(g.right);
		expect(detail.left).toBeGreaterThanOrEqual(g.right);
	});

	test('coverage and the next action come before the graph', async () => {
		render(StoryWithGraph, { story: goldenStory('implementation-merged') });
		const g = page.getByTestId('graph').element().getBoundingClientRect();
		for (const name of ['Coverage', 'Next action']) {
			const box = page.getByRole('region', { name }).element().getBoundingClientRect();
			expect(box.bottom).toBeLessThanOrEqual(g.top);
		}
	});
});
