import type { Correlation, Ref, Story } from './model';
import { sameRef } from './keys';

/** A correlation seen from one of its ends. */
export interface Link {
	correlation: Correlation;
	/** The end that is not the record looked at. */
	other: Ref;
	/**
	 * For an ambiguous correlation, each set of alternatives it is one of,
	 * of two or more records, none chosen: the records of one kind one
	 * record is ambiguously linked to, from it (an edit two runs could have
	 * made) or to it (two runs an announcement could be from, each linked
	 * to the task). Empty when the other candidates are not in the story:
	 * the collector drops a correlation whose other end is outside it.
	 */
	candidateSets: Ref[][];
}

function ambiguousGroup(story: Story, c: Correlation, shared: 'from' | 'to'): Ref[] {
	const varied = shared === 'from' ? 'to' : 'from';
	return story.correlations
		.filter(
			(x) => x.ambiguous && sameRef(x[shared], c[shared]) && x[varied].kind === c[varied].kind
		)
		.map((x) => x[varied]);
}

function candidateSetsOf(story: Story, c: Correlation): Ref[][] {
	if (!c.ambiguous) {
		return [];
	}
	return [ambiguousGroup(story, c, 'from'), ambiguousGroup(story, c, 'to')].filter(
		(set) => set.length > 1
	);
}

/** The correlations with ref at either end, in the story's order. */
export function linksOf(story: Story, ref: Ref): Link[] {
	const links: Link[] = [];
	for (const c of story.correlations) {
		const from = sameRef(c.from, ref);
		if (from || sameRef(c.to, ref)) {
			links.push({
				correlation: c,
				other: from ? c.to : c.from,
				candidateSets: candidateSetsOf(story, c)
			});
		}
	}
	return links;
}
