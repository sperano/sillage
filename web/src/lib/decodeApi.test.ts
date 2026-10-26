import { describe, expect, test } from 'vitest';
import { decode } from './check';
import { configResponse, freshnessResponse, resolution, searchResponse } from './decodeApi';

const TASK = { task: { instance: 'vikunja.example.com', id: 102 } };
const READ = { newestRead: '2026-03-03T20:00:00Z', oldestRead: '2026-03-02T20:00:00Z' };

function freshness(fields: Record<string, unknown>) {
	return decode(freshnessResponse, {
		sources: [{ source: 'kelos', instance: 'cluster-a', roots: 2, neverRead: 0, ...fields }]
	});
}

describe('search', () => {
	test('an empty query is the server listing the latest roots', () => {
		expect(decode(searchResponse, { query: '', roots: [{ root: TASK }] }).roots).toHaveLength(1);
	});

	test('a root twice, or a field the server never writes, fails', () => {
		expect(() =>
			decode(searchResponse, { query: 'w', roots: [{ root: TASK }, { root: TASK }] })
		).toThrow(/duplicate record/);
		expect(() => decode(searchResponse, { query: 'w', roots: [], next: 'x' })).toThrow(
			/unexpected field next/
		);
	});
});

describe('resolution', () => {
	test('a kind the server never answers fails', () => {
		expect(() => decode(resolution, { kind: 'jira-issue', roots: [{ root: TASK }] })).toThrow(
			/unknown value "jira-issue"/
		);
	});
});

describe('freshness', () => {
	test('a read root has both reads', () => {
		expect(freshness(READ).sources[0]).toMatchObject(READ);
		expect(() => freshness({ newestRead: READ.newestRead })).toThrow(
			/exactly when a root was read/
		);
		expect(() => freshness({})).toThrow(/exactly when a root was read/);
	});

	test('roots none of which was read have no read', () => {
		expect(freshness({ neverRead: 2 }).sources[0]?.newestRead).toBeUndefined();
		expect(() => freshness({ neverRead: 2, ...READ })).toThrow(/exactly when a root was read/);
	});

	test('counts and reads the store cannot answer fail', () => {
		expect(() => freshness({ neverRead: 3 })).toThrow(/more roots never read than roots/);
		expect(() => freshness({ roots: 0, neverRead: 0 })).toThrow(/positive integer/);
		expect(() => freshness({ newestRead: READ.oldestRead, oldestRead: READ.newestRead })).toThrow(
			/the oldest read is after the newest/
		);
	});

	test('one record per source instance', () => {
		const one = { source: 'wiki', instance: 'wiki.example.com', roots: 1, neverRead: 1 };
		expect(() => decode(freshnessResponse, { sources: [one, one] })).toThrow(/duplicate record/);
	});
});

describe('config', () => {
	test('no console is an empty map', () => {
		expect(decode(configResponse, { kelosConsoles: {} })).toEqual({ kelosConsoles: {} });
	});

	test('a missing map, an empty URL or a field the server never writes fails', () => {
		expect(() => decode(configResponse, {})).toThrow(/kelosConsoles/);
		expect(() => decode(configResponse, { kelosConsoles: { 'cluster-a': '' } })).toThrow(
			/cluster-a/
		);
		expect(() => decode(configResponse, { kelosConsoles: {}, wiki: 'x' })).toThrow(
			/unexpected field wiki/
		);
	});
});
