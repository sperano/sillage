// The read API's own responses, besides stories and the overview: what
// internal/api writes for a search, a resolved link, the sources'
// freshness and the frontend's configuration, with the runtime checks of that JSON. Each mirrors what the
// Go handler and store guarantee, so that a body they would never write
// fails here rather than rendering wrong.

import {
	type Decoder,
	anyString,
	array,
	compareTimes,
	fail,
	nonEmpty,
	nonNegativeInteger,
	object,
	oneOf,
	positiveInteger,
	record,
	refine,
	text,
	timestamp,
	unique
} from './check';
import { root, source } from './decodeModel';
import type { LinkConfig } from './links';
import { type Root, type Source, coverageKey } from './model';
import { rootSegments } from './roots';

/** A stored root a search or a link matched (Go's store.RootSummary). */
export interface RootSummary {
	root: Root;
	title?: string;
	/** The latest time of the root's records; absent when none has one. */
	lastActivity?: string;
}

/** The answer to a search (Go's api.SearchResponse). */
export interface SearchResponse {
	/** The query as the server read it: trimmed. */
	query: string;
	/** The roots it matches, most recently active first. */
	roots: RootSummary[];
}

/** The kinds of link the server resolves (Go's api.LinkKind). */
export const LINK_KINDS = [
	'vikunja-task',
	'github-pull-request',
	'wiki-page',
	'kelos-task'
] as const;
export type LinkKind = (typeof LINK_KINDS)[number];

/** A resolved link (Go's api.ResolveResponse). */
export interface Resolution {
	/** What the link was read as. */
	kind: LinkKind;
	/** The roots whose stories hold what it points to, most recently active first: one at least. */
	roots: RootSummary[];
}

/**
 * How recently one source instance was read, across the stored roots
 * (Go's store.SourceFreshness). Missing records are per root: a root's
 * story has them.
 */
export interface SourceFreshness {
	source: Source;
	instance: string;
	/** How many stored roots have coverage of the instance. */
	roots: number;
	/** How many of them record no successful read. */
	neverRead: number;
	/** The latest successful read of any of them; absent when none was read. */
	newestRead?: string;
	/** The stalest one's latest successful read; absent when none was read. */
	oldestRead?: string;
}

const rootSummary: Decoder<RootSummary> = object(
	{ root },
	{ title: text, lastActivity: timestamp }
);

const rootSummaries: Decoder<RootSummary[]> = unique(array(rootSummary), (s) =>
	rootSegments(s.root)
);

export const searchResponse: Decoder<SearchResponse> = object({
	query: anyString,
	roots: rootSummaries
});

// The server answers 404 rather than a resolution with no root.
export const resolution: Decoder<Resolution> = object({
	kind: oneOf(LINK_KINDS),
	roots: nonEmpty(rootSummaries)
});

// One row of the store's GROUP BY over coverage: a root at least, the
// never-read ones among them, and both reads exactly when one of them was
// read.
const sourceFreshness: Decoder<SourceFreshness> = refine(
	object(
		{ source, instance: text, roots: positiveInteger, neverRead: nonNegativeInteger },
		{ newestRead: timestamp, oldestRead: timestamp }
	),
	(f, path) => {
		if (f.neverRead > f.roots) fail(path, 'more roots never read than roots');
		const read = f.neverRead < f.roots;
		if ((f.newestRead !== undefined) !== read || (f.oldestRead !== undefined) !== read) {
			fail(path, 'the newest and oldest reads are there exactly when a root was read');
		}
		if (f.newestRead && f.oldestRead && compareTimes(f.oldestRead, f.newestRead) > 0) {
			fail(path, 'the oldest read is after the newest');
		}
	}
);

/** The sources' freshness (Go's api.FreshnessResponse): one record per source instance. */
export const freshnessResponse: Decoder<{ sources: SourceFreshness[] }> = object({
	sources: unique(array(sourceFreshness), (f) => coverageKey(f.source, f.instance))
});

/**
 * The frontend's configuration (Go's api.ConfigResponse): the Kelos
 * console's base URL per cluster name, empty when none is configured. A URL
 * the links refuse (not http or https) is the links' to drop, not a failure.
 */
export const configResponse: Decoder<Required<LinkConfig>> = object({
	kelosConsoles: record(text)
});
