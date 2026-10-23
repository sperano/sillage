import type { Correlation, Ref, Story } from './model';
import { sameRef } from './keys';

/** A correlation seen from one of its ends. */
export interface Link {
	correlation: Correlation;
	/** The end that is not the record looked at. */
	other: Ref;
	/**
	 * For an ambiguous correlation, the alternatives it is one of, two or
	 * more refs, none chosen. Outgoing alternatives first: the `to` ends of
	 * the ambiguous correlations leaving its `from`, of its `to`'s kind (the
	 * runs an edit could be from). Else incoming alternatives: the `from`
	 * ends of the ambiguous correlations entering its `to`, of its `from`'s
	 * kind, on identical evidence (the runs an announcement naming no pod
	 * could be from, each joined to the task). Empty when no other
	 * alternative is ambiguous: stronger evidence (a Task label) can have
	 * made a sibling's link certain. A list, so that a later rule can yield
	 * several sets.
	 */
	candidateSets: Ref[][];
}

function sameEvidence(a: Correlation, b: Correlation): boolean {
	return JSON.stringify(a.evidence) === JSON.stringify(b.evidence);
}

/** The other ends of the ambiguous correlations from c's from, of c's to's kind. */
function fanOut(story: Story, c: Correlation): Ref[] {
	return story.correlations
		.filter((x) => x.ambiguous && sameRef(x.from, c.from) && x.to.kind === c.to.kind)
		.map((x) => x.to);
}

/**
 * The other ends of the ambiguous correlations into c's to, of c's from's
 * kind, that rest on the same evidence: one record joined to each of
 * several candidates, as each candidate run to the task of an
 * announcement naming no pod. Two events each ambiguous between the same
 * runs also share an end, but each rests on its own evidence, and they are
 * not alternatives to each other.
 */
function fanIn(story: Story, c: Correlation): Ref[] {
	return story.correlations
		.filter(
			(x) => x.ambiguous && sameRef(x.to, c.to) && x.from.kind === c.from.kind && sameEvidence(x, c)
		)
		.map((x) => x.from);
}

/** The set of alternatives c is one of, fanning out first, else in; none when neither has two. */
function candidateSetsOf(story: Story, c: Correlation): Ref[][] {
	if (!c.ambiguous) {
		return [];
	}
	const out = fanOut(story, c);
	if (out.length > 1) {
		return [out];
	}
	const into = fanIn(story, c);
	return into.length > 1 ? [into] : [];
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
