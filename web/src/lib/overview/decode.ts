// Runtime checks of the overview JSON in model.ts, built from the story
// decoders: a run, an interval or a coverage record in a row is checked as
// it is in a story.

import {
	type Decoder,
	array,
	bool,
	compareTimes,
	decode,
	fail,
	nonNegativeInteger,
	object,
	oneOf,
	refine,
	text,
	timestamp,
	unique
} from '../check';
import { coverageList, interval, prKey, root, run } from '../decodeModel';
import { rootSegments } from '../roots';
import {
	MARK_KINDS,
	PR_STATES,
	type Mark,
	type OverviewPage,
	type OverviewPullRequest,
	type OverviewRow,
	type ReviewWait,
	type Window
} from './model';

const windowDecoder: Decoder<Window> = refine(
	object({ from: timestamp, to: timestamp }),
	(w, path) => {
		if (compareTimes(w.to, w.from) <= 0) fail(path, 'a window ends after it starts');
	}
);

const pullRequest: Decoder<OverviewPullRequest> = object({
	key: prKey,
	branch: text,
	periods: array(interval),
	state: oneOf(PR_STATES)
});

const reviewWait: Decoder<ReviewWait> = object(
	{ pr: prKey, request: text, interval },
	{ reviewer: text, team: text }
);

const mark: Decoder<Mark> = object(
	{ kind: oneOf(MARK_KINDS), at: timestamp, event: text },
	{ pr: prKey, pod: text }
);

const row: Decoder<OverviewRow> = object(
	{
		root,
		runs: unique(array(run), (r) => JSON.stringify(r.key)),
		pullRequests: unique(array(pullRequest), (p) => JSON.stringify(p.key)),
		reviewWaits: unique(array(reviewWait), (w) => w.request),
		marks: unique(array(mark), (m) => m.event),
		coverage: coverageList,
		missingHistory: bool,
		lastActivity: timestamp
	},
	{ title: text, projects: array(text) }
);

const page: Decoder<OverviewPage> = refine(
	object(
		{
			window: windowDecoder,
			rows: unique(array(row), (r) => rootSegments(r.root)),
			total: nonNegativeInteger
		},
		{ next: text }
	),
	(p, path) => {
		if (p.rows.length > p.total) fail(path, 'more rows than the total');
	}
);

/** Checks value against the overview page JSON, throwing a DecodeError on a mismatch. */
export function parseOverviewPage(value: unknown): OverviewPage {
	return decode(page, value);
}
