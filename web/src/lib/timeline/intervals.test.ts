import { describe, expect, test } from 'vitest';
import { prEvent, run, PR } from '../testing/builders';
import { checkSpan, reviewWaits, runSpans, spanDescription, spanText } from './intervals';

const READ = Date.parse('2026-03-01T12:00:00Z');
const at = (s: string) => Date.parse(s);

describe('runSpans', () => {
	test('a created, started and completed run waits, then runs, then exits', () => {
		const spans = runSpans(run({ created: '2026-03-01T09:58:00Z' }), READ);
		expect(spans.map((s) => [s.phase, s.text, s.start, s.end])).toEqual([
			['waiting', 'waiting to start', at('2026-03-01T09:58:00Z'), at('2026-03-01T10:00:00Z')],
			['running', 'running', at('2026-03-01T10:00:00Z'), at('2026-03-01T10:10:00Z')]
		]);
		expect(spans[1]!.exit).toEqual({
			mark: 'run-exit',
			label: 'run exit: succeeded',
			at: at('2026-03-01T10:10:00Z')
		});
	});

	test('a run still running is open until the last read, with no exit', () => {
		const [span] = runSpans(
			run({
				outcome: 'running',
				interval: { start: { state: 'known', at: '2026-03-01T10:00:00Z' }, end: { state: 'open' } }
			}),
			READ
		);
		expect(span).toMatchObject({ endState: 'open', openUntil: READ, exit: undefined });
	});

	test('a queued run waits from its creation', () => {
		const [span] = runSpans(
			run({
				outcome: 'waiting',
				created: '2026-03-01T09:00:00Z',
				interval: { start: { state: 'unknown' }, end: { state: 'open' } }
			}),
			READ
		);
		expect(span).toMatchObject({
			phase: 'waiting',
			text: 'waiting (queued)',
			start: at('2026-03-01T09:00:00Z')
		});
	});

	test('unknown bounds stay unknown, with no time and no exit', () => {
		const [span] = runSpans(
			run({
				outcome: 'unknown',
				interval: { start: { state: 'unknown' }, end: { state: 'unknown' } }
			}),
			READ
		);
		expect(span).toMatchObject({
			startState: 'unknown',
			start: undefined,
			endState: 'unknown',
			end: undefined,
			exit: undefined
		});
		expect(spanText(span!)).toBe('running');
	});
});

describe('checkSpan', () => {
	test('a successful check ends in a check-success mark', () => {
		const span = checkSpan(
			PR,
			{
				id: 1,
				sha: 'a'.repeat(40),
				name: 'test',
				conclusion: 'success',
				interval: {
					start: { state: 'known', at: '2026-03-01T10:00:00Z' },
					end: { state: 'known', at: '2026-03-01T10:03:00Z' }
				}
			},
			READ
		);
		expect(span.exit).toMatchObject({ mark: 'check-success', label: 'check test: success' });
		expect(spanText(span)).toBe('check test · 3m 00s');
	});

	test('a pending check is open', () => {
		const span = checkSpan(
			PR,
			{
				id: 2,
				sha: 'a'.repeat(40),
				name: 'lint',
				conclusion: 'pending',
				interval: { start: { state: 'known', at: '2026-03-01T10:00:00Z' }, end: { state: 'open' } }
			},
			READ
		);
		expect(span).toMatchObject({ endState: 'open', openUntil: READ, exit: undefined });
	});
});

describe('reviewWaits', () => {
	const request = prEvent('pr.review_requested', '2026-03-01T10:00:00Z', {
		details: { reviewer: 'alice' }
	});

	test('ends at the requested reviewer review', () => {
		const other = prEvent('pr.review_submitted', '2026-03-01T10:30:00Z', {
			actor: { login: 'bob', kind: 'person' }
		});
		const review = prEvent('pr.review_submitted', '2026-03-01T11:00:00Z', {
			actor: { login: 'alice', kind: 'person' }
		});
		const [wait] = reviewWaits([review, other, request], READ);
		expect(wait).toMatchObject({
			phase: 'review-wait',
			text: 'review wait: alice',
			endState: 'known',
			end: at('2026-03-01T11:00:00Z')
		});
	});

	test('ends at the request removal', () => {
		const removed = prEvent('pr.review_request_removed', '2026-03-01T10:20:00Z', {
			details: { reviewer: 'alice' }
		});
		expect(reviewWaits([request, removed], READ)[0]).toMatchObject({
			endState: 'known',
			end: at('2026-03-01T10:20:00Z')
		});
	});

	test('is open at the last read when unanswered', () => {
		expect(reviewWaits([request], READ)[0]).toMatchObject({ endState: 'open', openUntil: READ });
	});

	test('has an unknown end when the pull request merged first, or was never read', () => {
		const merged = prEvent('pr.merged', '2026-03-01T10:40:00Z');
		expect(reviewWaits([request, merged], READ)[0]).toMatchObject({
			endState: 'unknown',
			end: undefined
		});
		expect(reviewWaits([request], undefined)[0]).toMatchObject({ endState: 'unknown' });
	});

	test('a team request ends only at its removal', () => {
		const team = prEvent('pr.review_requested', '2026-03-01T10:00:00Z', {
			details: { team: 'core' }
		});
		const review = prEvent('pr.review_submitted', '2026-03-01T10:10:00Z', {
			actor: { login: 'core', kind: 'person' }
		});
		expect(reviewWaits([team, review], READ)[0]).toMatchObject({
			text: 'review wait: team core',
			endState: 'open'
		});
	});

	test('draws no wait without a request', () => {
		expect(reviewWaits([prEvent('pr.opened', '2026-03-01T09:00:00Z')], READ)).toEqual([]);
	});
});

test('spanDescription says what is known of each end', () => {
	const [span] = runSpans(
		run({
			outcome: 'failed',
			interval: { start: { state: 'unknown' }, end: { state: 'known', at: '2026-03-01T10:10:00Z' } }
		}),
		READ
	);
	expect(spanDescription(span!, 'utc')).toBe(
		'running, start unknown, to 2026-03-01 10:10:00 UTC, run exit: failed'
	);
});
