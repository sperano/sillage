import { describe, expect, it } from 'vitest';
import { DecodeError } from '../check';
import { CLOSED_UNANSWERED, overviewGoldenJSON } from '../testing/overview';
import { parseOverviewPage } from './decode';

type JSONObject = Record<string, unknown>;

/** The golden overview's JSON, its first row handed to change for a test. */
function withFirstRow(change: (row: JSONObject) => void): unknown {
	const page = overviewGoldenJSON() as { rows: JSONObject[] };
	change(page.rows[0]!);
	return page;
}

/** The golden overview's JSON, the reopened task's completion handed to change. */
function withReopened(change: (completion: JSONObject) => void): unknown {
	const page = overviewGoldenJSON() as { rows: JSONObject[] };
	const row = page.rows.find((r) => (r.root as { task?: { id: number } }).task?.id === 208);
	if (row === undefined) throw new Error('the golden overview has no task 208');
	change(row.completion as JSONObject);
	return page;
}

describe('parseOverviewPage', () => {
	it('decodes the golden overview unchanged', () => {
		const json = overviewGoldenJSON();
		expect(parseOverviewPage(json)).toEqual(json);
	});

	it('rejects a field the types do not know', () => {
		const json = withFirstRow((row) => (row.idleTime = '5m'));
		expect(() => parseOverviewPage(json)).toThrow(/unexpected field idleTime/);
	});

	it('rejects a pull request state it does not know', () => {
		const json = withFirstRow((row) => {
			(row.pullRequests as JSONObject[])[0]!.state = 'draft';
		});
		expect(() => parseOverviewPage(json)).toThrow(DecodeError);
	});

	it('checks runs as a story does', () => {
		const json = withFirstRow((row) => {
			const run = (row.runs as JSONObject[])[0]!;
			run.outcome = 'running';
		});
		expect(() => parseOverviewPage(json)).toThrow(/open end/);
	});

	it('rejects one attempt twice: keyed by UID and by the pod it names', () => {
		const json = withFirstRow((row) => {
			const run = (row.runs as JSONObject[])[0]!;
			const key = run.key as JSONObject;
			const byPod = {
				...run,
				key: { cluster: key.cluster, namespace: key.namespace, pod: run.pod }
			};
			(row.runs as JSONObject[]).push(byPod);
		});
		expect(() => parseOverviewPage(json)).toThrow(/names the same pod/);
	});

	it('rejects a mark of another source than its event', () => {
		const json = withFirstRow((row) => {
			(row.marks as JSONObject[])[0]!.instance = 'other.example.com';
		});
		expect(() => parseOverviewPage(json)).toThrow(/another source or instance/);
		const kind = withFirstRow((row) => {
			(row.marks as JSONObject[])[0]!.source = 'kelos';
		});
		expect(() => parseOverviewPage(kind)).toThrow(/cannot come from kelos/);
	});

	it('takes a close time exactly with an unknown end, and not before the request', () => {
		const wait = (row: JSONObject) => (row.reviewWaits as JSONObject[])[0]!;
		const unclosed = withFirstRow((row) => {
			(wait(row).interval as JSONObject).end = { state: 'unknown' };
		});
		expect(() => parseOverviewPage(unclosed)).toThrow(/close time exactly when/);
		const known = withFirstRow((row) => {
			wait(row).closed = CLOSED_UNANSWERED;
		});
		expect(() => parseOverviewPage(known)).toThrow(/close time exactly when/);
		const early = withFirstRow((row) => {
			(wait(row).interval as JSONObject).end = { state: 'unknown' };
			wait(row).closed = '2026-03-10T14:00:00Z';
		});
		expect(() => parseOverviewPage(early)).toThrow(/closed before the request/);
	});

	it('takes a reopening exactly with a reopened state, after the completion and its last read', () => {
		const done = withReopened((c) => (c.state = 'done'));
		expect(() => parseOverviewPage(done)).toThrow(/reopening exactly when/);
		const bare = withReopened((c) => delete c.reopened);
		expect(() => parseOverviewPage(bare)).toThrow(/reopening exactly when/);
		const early = withReopened((c) => {
			(c.reopened as JSONObject).after = '2026-03-10T13:00:00Z';
		});
		expect(() => parseOverviewPage(early)).toThrow(/before the completion/);
		const unseen = withReopened((c) => {
			(c.reopened as JSONObject).by = (c.reopened as JSONObject).after;
		});
		expect(() => parseOverviewPage(unseen)).toThrow(/only after it was seen done/);
		const state = withReopened((c) => (c.state = 'open'));
		expect(() => parseOverviewPage(state)).toThrow(DecodeError);
	});

	it('rejects a window that ends before it starts', () => {
		const json = overviewGoldenJSON() as { window: { from: string; to: string } };
		json.window = { from: json.window.to, to: json.window.from };
		expect(() => parseOverviewPage(json)).toThrow(/ends after it starts/);
	});

	it('rejects more rows than the total', () => {
		const json = overviewGoldenJSON() as { total: number };
		json.total = 1;
		expect(() => parseOverviewPage(json)).toThrow(/more rows than the total/);
	});
});
