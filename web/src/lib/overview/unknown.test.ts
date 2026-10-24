import { describe, expect, it } from 'vitest';
import { afternoon, overviewGolden, rowOfTask } from '../testing/overview';
import { rowSources, rowUnknownSpans } from './unknown';

const page = overviewGolden();
const range = { start: afternoon('14:00'), end: afternoon('15:00') };

describe('rowUnknownSpans', () => {
	it('hatches the Kelos gap of a row', () => {
		const spans = rowUnknownSpans(rowOfTask(page, 205), range);
		expect(spans.map((s) => [s.start, s.end])).toEqual([[afternoon('14:00'), afternoon('14:50')]]);
		expect(spans[0]!.reason).toMatch(/^kelos\/cluster-a: the Kelos watch expired/);
	});

	it('hatches nothing where every source was read through the window', () => {
		expect(rowUnknownSpans(rowOfTask(page, 201), range)).toEqual([]);
	});

	it('counts the source of a mark, with no other record from it', () => {
		const row = rowOfTask(page, 206);
		const uncovered = { ...row, coverage: row.coverage.filter((c) => c.source !== 'vikunja') };
		expect(rowSources(uncovered)).toContain('vikunja/vikunja.example.com');
		expect(rowUnknownSpans(uncovered, range)).toEqual([
			{ ...range, reason: 'vikunja/vikunja.example.com: no coverage recorded' }
		]);
	});

	it('hatches the whole window for a source a row has records from but no coverage of', () => {
		const row = rowOfTask(page, 203);
		const uncovered = { ...row, coverage: row.coverage.filter((c) => c.source !== 'kelos') };
		expect(rowSources(uncovered)).toContain('kelos/cluster-a');
		expect(rowUnknownSpans(uncovered, range)).toEqual([
			{ ...range, reason: 'kelos/cluster-a: no coverage recorded' }
		]);
	});
});
