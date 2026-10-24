import { describe, expect, it } from 'vitest';
import type { Run } from '../model';
import { afternoon, overviewGolden, rowOfTask } from '../testing/overview';
import { byExecutor, collapsedLane, expandedLanes, rowTitles } from './lanes';

const page = overviewGolden();
const range = { start: afternoon('14:00'), end: afternoon('15:00') };

describe('lanes', () => {
	it('collapses a row into one lane holding all its spans', () => {
		const lane = collapsedLane(rowOfTask(page, 201));
		expect(lane.spans.map((s) => s.kind)).toEqual(['pr', 'review', 'waiting', 'running']);
	});

	it('expands a retry into one lane per attempt, the failure kept', () => {
		const lanes = expandedLanes(rowOfTask(page, 203), range);
		expect(lanes.map((l) => l.label)).toEqual([
			'agent-cedar · attempt 1 · failed',
			'agent-cedar · attempt 2 · running'
		]);
		expect(lanes.map((l) => l.marks.map((m) => m.pod))).toEqual([
			['vikunja-gadgets-agent-cedar-203-ccccc'],
			['vikunja-gadgets-agent-cedar-203-ddddd']
		]);
	});

	it('expands pull requests, review waits and the task own marks', () => {
		expect(expandedLanes(rowOfTask(page, 201), range).map((l) => l.label)).toEqual([
			'agent-cedar · succeeded',
			'PR #21 · example-org/widgets',
			'review on PR #21: owner-gh'
		]);
		expect(
			expandedLanes(rowOfTask(page, 206), range).map((l) => [l.label, l.marks.map((m) => m.kind)])
		).toEqual([
			['PR #24 · example-org/widgets', ['pr.review_submitted', 'pr.merged']],
			['task', ['task.done']]
		]);
	});

	it('says why a run with no known bound draws nothing', () => {
		expect(expandedLanes(rowOfTask(page, 205), range)[0]?.note).toBe(
			'start and end unknown: no bar'
		);
	});

	it('says a run that started before the window with its end unknown may run on', () => {
		const row = rowOfTask(page, 205);
		const run: Run = {
			...row.runs[0]!,
			interval: { start: { state: 'known', at: '2026-03-10T13:30:00Z' }, end: { state: 'unknown' } }
		};
		expect(expandedLanes({ ...row, runs: [run] }, range)[0]?.note).toBe(
			'started before this window, end unknown: no bar'
		);
	});

	it('says why an earlier failed attempt before the window draws nothing', () => {
		const late = { start: afternoon('14:38'), end: range.end };
		const [failed, retry] = expandedLanes(rowOfTask(page, 203), late);
		expect([failed?.note, retry?.note]).toEqual(['before this window: no bar', undefined]);
	});
});

describe('byExecutor', () => {
	it('keeps every run of one profile its own lane, the simultaneous ones included', () => {
		const groups = byExecutor(page.rows, range);
		expect(groups.map((g) => [g.executor, g.lanes.map((l) => l.label)])).toEqual([
			['agent-birch', ['Docs navigation · succeeded']],
			[
				'agent-cedar',
				[
					'Cache metrics · attempt 1 · failed',
					'Health check · succeeded',
					'Cache metrics · attempt 2 · running'
				]
			],
			['agent-maple', ['Route rules · succeeded', 'Storage cleanup · unknown']]
		]);
		expect(groups[1]!.lanes.map((l) => l.owner?.root.task?.id)).toEqual([203, 201, 203]);
	});

	it('names an untitled task by its instance when another shares its id', () => {
		const a = { ...rowOfTask(page, 201), title: undefined };
		const other = { ...a, root: { task: { instance: 'tasks.example.com', id: 201 } } };
		const alone = { ...rowOfTask(page, 202), title: undefined };
		const titles = rowTitles([a, other, alone]);
		expect([titles.get(a), titles.get(other), titles.get(alone)]).toEqual([
			'task 201 on vikunja.example.com',
			'task 201 on tasks.example.com',
			'task 202'
		]);
		const runs: Run[] = other.runs.map((r) => ({ ...r, key: { ...r.key, cluster: 'cluster-b' } }));
		const labels = byExecutor([a, { ...other, runs }], range).flatMap((g) =>
			g.lanes.map((l) => l.label)
		);
		expect(labels).toEqual([
			'task 201 on vikunja.example.com · succeeded',
			'task 201 on tasks.example.com · succeeded'
		]);
	});
});
