import type { Correlation, Ref, Story } from './model';
import { sameRef } from './keys';

/** A correlation seen from one of its ends. */
export interface Link {
	correlation: Correlation;
	/** The end that is not the record looked at. */
	other: Ref;
	/**
	 * For an ambiguous correlation, every candidate the evidence fits: the
	 * other ends of the ambiguous correlations from the same record. None
	 * was chosen.
	 */
	candidates: Ref[];
}

function candidatesOf(story: Story, c: Correlation): Ref[] {
	if (!c.ambiguous) {
		return [];
	}
	return story.correlations.filter((x) => x.ambiguous && sameRef(x.from, c.from)).map((x) => x.to);
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
				candidates: candidatesOf(story, c)
			});
		}
	}
	return links;
}
