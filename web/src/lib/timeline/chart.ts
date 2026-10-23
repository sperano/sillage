// The chart's vertical layout: the axis, one coverage band per source,
// then each lane's heading and rows.

import type { Story } from '../model';
import { unknownSpans, type UnknownSpan } from './coverage';
import { SECTION_TITLES, type Row, type Section } from './lanes';
import { layoutRow, type RowLayout } from './layout';
import type { Zone } from './time';
import type { Scale, TimeWindow } from './view';

/** The height of the time axis at the top. */
export const AXIS_PX = 30;
/** The height of one source's coverage band. */
export const SOURCE_ROW_PX = 22;
/** The height of a lane's heading. */
export const SECTION_PX = 22;

export interface SourceBand {
	source: string;
	y: number;
	unknown: UnknownSpan[];
}

export interface SectionHeading {
	section: Section;
	title: string;
	y: number;
}

export interface RowChart extends RowLayout {
	/** Where the row's sources are unknown: shaded under its marks. */
	unknown: UnknownSpan[];
}

export interface Chart {
	sources: SourceBand[];
	sections: SectionHeading[];
	rows: RowChart[];
	height: number;
}

export interface ChartInput {
	story: Story;
	rows: Row[];
	sources: string[];
	domain: TimeWindow;
	scale: Scale;
	zone: Zone;
	expanded: ReadonlySet<string>;
	timeText: (t: number) => string;
}

/** Lays the chart out top to bottom. */
export function layoutChart(input: ChartInput): Chart {
	const { story, domain, zone } = input;
	const unknownOf = new Map(
		input.sources.map((s) => [s, unknownSpans(story, s, domain, zone)] as const)
	);
	let y = AXIS_PX;
	const sources = input.sources.map((source) => {
		const band = { source, y, unknown: unknownOf.get(source) ?? [] };
		y += SOURCE_ROW_PX;
		return band;
	});
	const sections: SectionHeading[] = [];
	const rows: RowChart[] = [];
	for (const row of input.rows) {
		if (sections[sections.length - 1]?.section !== row.section) {
			sections.push({ section: row.section, title: SECTION_TITLES[row.section], y });
			y += SECTION_PX;
		}
		const laid = layoutRow(row, y, input.scale, input.expanded, input.timeText);
		rows.push({ ...laid, unknown: row.sources.flatMap((s) => unknownOf.get(s) ?? []) });
		y += laid.height;
	}
	return { sources, sections, rows, height: y };
}
