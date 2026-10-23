import { describe, expect, it } from 'vitest';
import { overviewGolden, rowOfTask } from '../testing/overview';
import { byExecutor, collapsedLane, expandedLanes } from './lanes';

const page = overviewGolden();

describe('lanes', () => {
	it('collapses a row into one lane holding all its spans', () => {
		const lane = collapsedLane(rowOfTask(page, 201));
		expect(lane.spans.map((s) => s.kind)).toEqual(['pr', 'review', 'waiting', 'running']);
	});

	it('expands a retry into one lane per attempt, the failure kept', () => {
		const lanes = expandedLanes(rowOfTask(page, 203));
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
		expect(expandedLanes(rowOfTask(page, 201)).map((l) => l.label)).toEqual([
			'agent-cedar · succeeded',
			'PR #21 · example-org/widgets',
			'review on PR #21: owner-gh'
		]);
		expect(expandedLanes(rowOfTask(page, 206)).map((l) => [l.label, l.marks.length])).toEqual([
			['PR #24 · example-org/widgets', 2],
			['task', 1]
		]);
	});

	it('says why a run with no known bound draws nothing', () => {
		expect(expandedLanes(rowOfTask(page, 205))[0]?.note).toBe('start and end unknown: no bar');
	});
});

describe('byExecutor', () => {
	it('keeps every run of one profile its own lane, the simultaneous ones included', () => {
		const groups = byExecutor(page.rows);
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
});
