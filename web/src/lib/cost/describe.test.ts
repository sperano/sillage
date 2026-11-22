import { expect, test } from 'vitest';
import { estimateText, noteText, reportedText, threadLabel, usd } from './describe';
import { COST_NOTES } from './model';

test.each([
	['18.886400', 'US$18.8864'],
	['4.340000', 'US$4.34'],
	['1.000000', 'US$1.00'],
	['0.000150', 'US$0.00015'],
	['0.000000', 'US$0.00'],
	['12', 'US$12.00']
])('usd(%j) is %j: exact, never rounded', (amount, want) => {
	expect(usd(amount)).toBe(want);
});

test('a complete estimate is its amount, a partial one a lower bound', () => {
	expect(estimateText({ status: 'complete', estimatedUSD: '13.546400' })).toBe('US$13.5464');
	expect(estimateText({ status: 'partial', estimatedUSD: '18.886400' })).toBe(
		'at least US$18.8864'
	);
});

test('without an estimate, a breakdown is unknown or none, never zero', () => {
	expect(estimateText({ status: 'unknown' })).toBe('unknown');
	expect(estimateText({ status: 'none' })).toBe('none: no model used');
	expect(estimateText({ status: 'complete' })).toBe('unknown');
	expect(estimateText({ status: 'partial' })).toBe('unknown');
});

test('a reported cost is the amount, or none reported, never zero', () => {
	expect(reportedText({ reportedUSD: '15.660000' })).toBe('US$15.66');
	expect(reportedText({})).toBe('none reported');
});

test('every note has a sentence of its own', () => {
	const texts = COST_NOTES.map(noteText);
	expect(texts.every((t, i) => t !== COST_NOTES[i] && t.endsWith('.'))).toBe(true);
	expect(new Set(texts).size).toBe(COST_NOTES.length);
});

test('a thread is its main loop, or a subagent and the one that started it', () => {
	expect(threadLabel({ stream: 'session-a1' })).toBe('session-a1, main loop');
	expect(threadLabel({ stream: 'session-a1', agent: 'agent-explore-1' })).toBe(
		'session-a1, subagent agent-explore-1'
	);
	expect(
		threadLabel({ stream: 'session-a1', agent: 'agent-review-2', parentAgent: 'agent-explore-1' })
	).toBe('session-a1, subagent agent-review-2, started by agent-explore-1');
});
