import { describe, expect, it } from 'vitest';
import { DecodeError } from '../check';
import { overviewGoldenJSON } from '../testing/overview';
import { parseOverviewPage } from './decode';

type JSONObject = Record<string, unknown>;

/** The golden overview's JSON, its first row handed to change for a test. */
function withFirstRow(change: (row: JSONObject) => void): unknown {
	const page = overviewGoldenJSON() as { rows: JSONObject[] };
	change(page.rows[0]!);
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
