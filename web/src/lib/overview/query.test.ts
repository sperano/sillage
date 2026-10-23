import { describe, expect, it } from 'vitest';
import {
	DEFAULT_QUERY,
	OVERVIEW_PATH,
	overviewApiParams,
	overviewHref,
	parseOverviewQuery
} from './query';

describe('the overview query', () => {
	it('is empty by default', () => {
		expect(parseOverviewQuery(new URLSearchParams())).toEqual(DEFAULT_QUERY);
		expect(overviewHref(DEFAULT_QUERY)).toBe(OVERVIEW_PATH);
	});

	it('round-trips through the page URL', () => {
		const q = {
			from: '2026-03-10T14:00:00Z',
			to: '2026-03-10T15:00:00Z',
			project: 'widgets',
			task: 'cache metrics',
			executor: 'agent-cedar',
			review: true,
			failed: true,
			missing: false,
			group: 'executor' as const
		};
		const href = overviewHref(q);
		expect(parseOverviewQuery(new URL(href, 'http://sillage.example.com').searchParams)).toEqual(q);
	});

	it('reads only true as a set flag, and only executor as the other grouping', () => {
		const q = parseOverviewQuery(new URLSearchParams('review=1&failed=yes&missing=true&group=x'));
		expect([q.review, q.failed, q.missing, q.group]).toEqual([false, false, true, 'task']);
	});

	it('sends the API the filters and the cursor, never the grouping', () => {
		const params = overviewApiParams(
			{ ...DEFAULT_QUERY, executor: 'agent-cedar', group: 'executor' },
			'c1'
		);
		expect(params.toString()).toBe('executor=agent-cedar&after=c1');
	});
});
