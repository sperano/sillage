import { expect, test } from 'vitest';
import { golden } from '../testing/goldens';
import { AXIS_PX, layoutChart, SECTION_PX, SOURCE_ROW_PX } from './chart';
import { storySources } from './coverage';
import { buildRows } from './lanes';
import { scaleOf, storyDomain } from './view';

function chartOf(name: string) {
	const story = golden(name);
	const rows = buildRows(story);
	const domain = storyDomain(story);
	return layoutChart({
		story,
		rows,
		sources: storySources(
			story,
			rows.map((r) => r.sources)
		),
		domain,
		scale: scaleOf(domain, 200, 1000),
		zone: 'utc',
		expanded: new Set(),
		timeText: String
	});
}

test('stacks the axis, one band per source, then each section heading and its rows', () => {
	const chart = chartOf('implementation-merged');
	expect(chart.sources.map((b) => [b.source, b.y])).toEqual([
		['github/github.example.com', AXIS_PX],
		['kelos/cluster-a', AXIS_PX + SOURCE_ROW_PX],
		['vikunja/vikunja.example.com', AXIS_PX + 2 * SOURCE_ROW_PX]
	]);
	expect(chart.sections.map((s) => s.title)).toEqual(['Task', 'Agent runs', 'Pull requests']);
	const first = chart.rows[0]!;
	expect(first.y).toBe(AXIS_PX + 3 * SOURCE_ROW_PX + SECTION_PX);
	for (const [a, b] of chart.rows.slice(0, -1).map((r, i) => [r, chart.rows[i + 1]!] as const)) {
		expect(b.y).toBeGreaterThanOrEqual(a.y + a.height);
	}
	const last = chart.rows[chart.rows.length - 1]!;
	expect(chart.height).toBe(last.y + last.height);
});

test('shades a row wherever one of its sources is unknown', () => {
	const chart = chartOf('announced-run-only');
	const run = chart.rows.find((r) => r.row.id.startsWith('run:'))!;
	expect(run.unknown.map((u) => u.reason)).toContain(
		'kelos/cluster-a: Kelos deletes a finished Task after the configured TTL: a run before then is read only from its TaskRecord, which only a run that reported usage leaves'
	);
});
