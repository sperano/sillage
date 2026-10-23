import { tick } from 'svelte';
import { beforeEach, describe, expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { eventKey, runKeyString, type Story } from '../model';
import { Selection } from '../selection.svelte';
import { event, PR, prEvent, run, story } from '../testing/builders';
import { golden, goldens } from '../testing/goldens';
import Timeline from './Timeline.svelte';

const STORIES = [
	'announced-run-only',
	'discuss-no-pr',
	'failed-run-retry',
	'implementation-merged',
	'wiki-rooted-request'
];

/** A desktop viewport: the default one is a phone's, too narrow for the bars' text. */
const VIEWPORT = { width: 1280, height: 900 };

beforeEach(async () => {
	await page.viewport(VIEWPORT.width, VIEWPORT.height);
});

async function show(s: Story, selection = new Selection()) {
	const { container } = await render(Timeline, { story: s, selection, zone: 'utc' });
	await tick();
	return { container, selection };
}

/** Expands every group of events, so that each event has its own mark. */
async function expandAll(container: Element) {
	for (const badge of container.querySelectorAll<SVGGElement>('[aria-expanded="false"]')) {
		badge.dispatchEvent(new MouseEvent('click', { bubbles: true }));
	}
	await tick();
}

function all(container: Element, selector: string): Element[] {
	return [...container.querySelectorAll(selector)];
}

test('the corpus holds every golden story', () => {
	expect([...goldens.keys()].sort()).toEqual(STORIES);
});

describe.each(STORIES)('%s', (name) => {
	test('draws every event, and every row of the story', async () => {
		const s = golden(name);
		const { container } = await show(s);
		await expandAll(container);
		const drawn = new Set(all(container, '[data-event]').map((e) => e.getAttribute('data-event')));
		for (const e of s.events) expect(drawn, eventKey(e)).toContain(eventKey(e));
		for (const r of s.runs) {
			expect(container.querySelector(`[data-row="run:${runKeyString(r.key)}"]`)).not.toBeNull();
		}
	});

	test('draws no bar across an unknown bound', async () => {
		const { container } = await show(golden(name));
		const bounds = all(container, '[data-bar]').map((bar) => {
			const span = bar.closest('[data-span]')!;
			return [span.getAttribute('data-start'), span.getAttribute('data-end')];
		});
		expect(
			bounds.every(([start, end]) => start === 'known' && (end === 'known' || end === 'open'))
		).toBe(true);
	});
});

describe('golden invariants', () => {
	test('the discussion story has no pull request lane', async () => {
		const { container } = await show(golden('discuss-no-pr'));
		expect(container.querySelector('[data-section="pulls"]')).toBeNull();
		expect(container.querySelector('[data-section="pages"]')).not.toBeNull();
	});

	test('the retry story has two run rows, one per attempt', async () => {
		const { container } = await show(golden('failed-run-retry'));
		const rows = all(container, '[data-row^="run:"]');
		expect(rows).toHaveLength(2);
		const exits = rows.map((r) =>
			r.querySelector('[data-span="running"]')?.getAttribute('aria-label')
		);
		expect(exits[0]).toContain('run exit: failed');
		expect(exits[1]).toContain('run exit: succeeded');
	});

	test('the announced-only run has no bar, says why, and its period is shaded unknown', async () => {
		const s = golden('announced-run-only');
		const { container } = await show(s);
		const row = container.querySelector(`[data-row="run:${runKeyString(s.runs[0]!.key)}"]`)!;
		expect(row.querySelector('[data-bar]')).toBeNull();
		expect(row.querySelector('[data-stub]')).toBeNull();
		expect(row.querySelector('[data-note]')?.textContent).toContain('start and end unknown');
		const reasons = all(row, '[data-unknown] title').map((t) => t.textContent);
		expect(reasons.some((r) => r?.includes('had expired'))).toBe(true);
	});

	test('the live wiki edit is present', async () => {
		const s = golden('wiki-rooted-request');
		const live = s.events.find((e) => e.details?.version === 'live')!;
		const { container } = await show(s);
		await expandAll(container);
		const mark = container.querySelector(`[data-event="${eventKey(live)}"]`);
		expect(mark?.getAttribute('aria-label')).toContain('page edited (live version)');
	});

	test('task done, run exit, check success and PR merged are separate marks', async () => {
		const { container } = await show(golden('implementation-merged'));
		await expandAll(container);
		for (const mark of ['task-done', 'run-exit', 'check-success', 'check-end', 'pr-merged']) {
			expect(container.querySelector(`[data-mark="${mark}"]`), mark).not.toBeNull();
		}
	});
});

describe('intervals', () => {
	test('an unknown bound is a dashed stub, never a bar', async () => {
		const r = run({
			outcome: 'failed',
			interval: { start: { state: 'unknown' }, end: { state: 'known', at: '2026-03-01T10:10:00Z' } }
		});
		const { container } = await show(story({ runs: [r] }));
		expect(container.querySelector('[data-bar]')).toBeNull();
		const stub = container.querySelector('[data-stub]')!.closest('[data-span]')!;
		expect(stub.textContent).toContain('running, start unknown');
	});

	test('an open end is drawn open', async () => {
		const r = run({
			outcome: 'running',
			interval: { start: { state: 'known', at: '2026-03-01T10:00:00Z' }, end: { state: 'open' } }
		});
		const s = story({
			runs: [r],
			coverage: [
				{ source: 'kelos', instance: 'cluster-a', lastSuccessfulRead: '2026-03-01T11:00:00Z' }
			]
		});
		const { container } = await show(s);
		const span = container.querySelector('[data-span="running"]')!;
		expect(span.getAttribute('data-end')).toBe('open');
		expect(span.getAttribute('aria-label')).toContain(
			'not ended at the last read, 2026-03-01 11:00:00 UTC'
		);
		expect(span.querySelector('[data-mark="run-exit"]')).toBeNull();
	});

	test('waiting, running and review waits say what they are', async () => {
		const s = story({
			runs: [run({ created: '2026-03-01T09:40:00Z' })],
			pullRequests: [{ key: PR, branch: 'b', checks: [] }],
			events: [
				prEvent('pr.review_requested', '2026-03-01T10:20:00Z', { details: { reviewer: 'alice' } }),
				prEvent('pr.review_submitted', '2026-03-01T10:50:00Z', {
					actor: { login: 'alice', kind: 'person' }
				})
			]
		});
		const { container } = await show(s);
		const labels = all(container, '[data-span]').map((e) => e.getAttribute('aria-label'));
		expect(labels.some((l) => l?.startsWith('waiting to start · 20m 00s'))).toBe(true);
		expect(labels.some((l) => l?.startsWith('running · 10m 00s'))).toBe(true);
		expect(labels.some((l) => l?.startsWith('review wait: alice · 30m 00s'))).toBe(true);
		expect(all(container, 'text').some((t) => t.textContent === 'running · 10m 00s')).toBe(true);
	});
});

describe('interaction', () => {
	test('selecting an event or an interval sets the shared selection', async () => {
		const s = golden('implementation-merged');
		const { container, selection } = await show(s);
		const runSpan = container.querySelector<SVGGElement>('[data-span="running"]')!;
		runSpan.dispatchEvent(new MouseEvent('click', { bubbles: true }));
		expect(selection.current).toEqual({ kind: 'run', key: runKeyString(s.runs[0]!.key) });
		await tick();
		expect(runSpan.getAttribute('aria-pressed')).toBe('true');

		const merged = s.events.find((e) => e.kind === 'pr.merged')!;
		await expandAll(container);
		const mark = container.querySelector<SVGGElement>(`[data-event="${eventKey(merged)}"]`)!;
		mark.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
		expect(selection.current).toEqual({ kind: 'event', key: eventKey(merged) });
	});

	test('a selection made elsewhere shows on the graph', async () => {
		const s = golden('discuss-no-pr');
		const selection = new Selection();
		const { container } = await show(s, selection);
		selection.select({ kind: 'run', key: runKeyString(s.runs[0]!.key) });
		await tick();
		expect(container.querySelector('[data-span="running"]')!.getAttribute('aria-pressed')).toBe(
			'true'
		);
	});

	test('one zone applies to the axis and every label', async () => {
		const { container } = await show(golden('implementation-merged'));
		const range = page.getByRole('status');
		await expect.element(range).toHaveTextContent(/UTC$/);
		const label = container.querySelector('[data-span="running"]')!.getAttribute('aria-label');
		expect(label).toContain('2026-03-03 18:05:37 UTC');
		await page.getByLabelText('Time zone').selectOptions('local');
		await expect.element(range).toHaveTextContent(/America\/Toronto$/);
		await expect
			.element(page.getByRole('button', { name: /^running · 25m 33s/ }))
			.toHaveAccessibleName(/2026-03-03 13:05:37 America\/Toronto/);
	});

	test('zooms in to a minute, back out to the whole story, and pans', async () => {
		await show(golden('implementation-merged'));
		const range = page.getByRole('status');
		const whole = range.element().textContent ?? '';
		const zoomIn = page.getByRole('button', { name: 'Zoom in' });
		const earlier = page.getByRole('button', { name: 'Earlier' });
		await expect.element(earlier).toBeDisabled();
		await zoomIn.click();
		await expect.element(range).not.toHaveTextContent(whole);
		await expect.element(earlier).toBeEnabled();
		await earlier.click();
		for (let i = 0; i < 12; i++) await zoomIn.click({ force: true });
		await expect.element(zoomIn).toBeDisabled();
		await page.getByRole('button', { name: 'Whole story' }).click();
		await expect.element(range).toHaveTextContent(whole);
	});

	test('groups crowded events under a count that expands', async () => {
		const at = (s: number) => `2026-03-01T10:00:${String(s).padStart(2, '0')}Z`;
		const crowded = [
			event({ sourceTime: at(0) }),
			event({ sourceTime: at(10) }),
			event({ sourceTime: at(20) })
		];
		const far = event({ sourceTime: '2026-03-02T10:00:00Z' });
		const { container } = await show(story({ events: [...crowded, far] }));
		const badge = page.getByRole('button', { name: /^3 events from/ });
		await expect.element(badge).toHaveAttribute('aria-expanded', 'false');
		expect(container.querySelector(`[data-event="${eventKey(crowded[0]!)}"]`)).toBeNull();
		await badge.click();
		await expect.element(badge).toHaveAttribute('aria-expanded', 'true');
		for (const e of crowded) {
			expect(container.querySelector(`[data-event="${eventKey(e)}"]`)).not.toBeNull();
		}
	});
});
