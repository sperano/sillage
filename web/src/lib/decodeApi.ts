// The read API's own responses, besides stories and the overview: what
// internal/api writes for a search, a resolved link, the sources'
// freshness, a root's webhook deliveries and the frontend's configuration, with the runtime checks of that JSON. Each mirrors what the
// Go handler and store guarantee, so that a body they would never write
// fails here rather than rendering wrong.

import {
	type Decoder,
	anyString,
	array,
	bool,
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
export const LINK_KINDS = ['vikunja-task', 'github-pull-request', 'kelos-task'] as const;
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

/**
 * A source sillage does not read, and why (Go's api.OffSource): its
 * configuration is incomplete, or the collector is off as a whole.
 */
export interface OffSource {
	source: Source;
	/** The source instance; absent when the configuration does not name it. */
	instance?: string;
	reason: string;
}

/** The sources' freshness, and those not read (Go's api.FreshnessResponse). */
export interface Freshness {
	/** One record per source instance a stored root's coverage names. */
	sources: SourceFreshness[];
	/** The sources off, one record per source instance. */
	off: OffSource[];
}

const offSource: Decoder<OffSource> = object({ source, reason: text }, { instance: text });

export const freshnessResponse: Decoder<Freshness> = object({
	sources: unique(array(sourceFreshness), (f) => coverageKey(f.source, f.instance)),
	off: unique(array(offSource), (o) => coverageKey(o.source, o.instance ?? ''))
});

/**
 * The frontend's configuration (Go's api.ConfigResponse): the Kelos
 * console's base URL per cluster name, empty when none is configured. A URL
 * the links refuse (not http or https) is the links' to drop, not a failure.
 */
export const configResponse: Decoder<Required<LinkConfig>> = object({
	kelosConsoles: record(text)
});

/** What sillage did with a webhook delivery attempt (Go's model.DeliveryOutcome). */
export const DELIVERY_OUTCOMES = ['accepted', 'ignored', 'rejected', 'failed'] as const;
export type DeliveryOutcome = (typeof DELIVERY_OUTCOMES)[number];

/**
 * A webhook delivery attempt sillage received that named a root (Go's
 * model.RootDelivery): evidence that sillage received it, at receivedAt,
 * and of nothing else. Not that the source's event happened then, not that
 * Kelos received its own delivery of the same event, and not that it
 * caused anything in the story.
 */
export interface Delivery {
	source: Source;
	instance: string;
	/** The source's delivery id: a redelivery reuses it. */
	id: string;
	/** The source's event name; absent when the body could not be read. */
	event?: string;
	receivedAt: string;
	outcome: DeliveryOutcome;
	/** Why it was ignored, rejected or failed. */
	detail?: string;
	/**
	 * When the first successful read of the root from the delivery's source
	 * that started at or after receivedAt started; absent while none has.
	 */
	nextRead?: string;
}

/** The deliveries that named a root, oldest first (Go's api.DeliveriesResponse). */
export interface Deliveries {
	root: Root;
	deliveries: Delivery[];
	/** More deliveries named the root than the latest ones listed. */
	truncated?: boolean;
}

const delivery: Decoder<Delivery> = refine(
	object(
		{
			source,
			instance: text,
			id: text,
			receivedAt: timestamp,
			outcome: oneOf(DELIVERY_OUTCOMES)
		},
		{ event: text, detail: text, nextRead: timestamp }
	),
	(d, path) => {
		if (d.nextRead !== undefined && compareTimes(d.nextRead, d.receivedAt) < 0) {
			fail(path, 'the next read started before the delivery was received');
		}
	}
);

export const deliveriesResponse: Decoder<Deliveries> = refine(
	object({ root, deliveries: array(delivery) }, { truncated: bool }),
	(r, path) => {
		r.deliveries.forEach((d, i) => {
			const prev = r.deliveries[i - 1];
			if (prev && compareTimes(prev.receivedAt, d.receivedAt) > 0) {
				fail(`${path}.deliveries[${i}]`, 'received before the delivery listed before it');
			}
		});
	}
);
