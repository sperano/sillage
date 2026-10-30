import { describe, expect, test } from 'vitest';
import type { Delivery } from './decodeApi';
import {
	deliveriesAround,
	deliveryKey,
	deliveryLabel,
	deliveryRef,
	findDelivery,
	isStateChange,
	outcomeText
} from './deliveries';
import { event } from './testing/builders';

function delivery(fields: Partial<Delivery> = {}): Delivery {
	return {
		source: 'github',
		instance: 'github.example.com',
		id: 'd-1',
		event: 'pull_request',
		receivedAt: '2026-03-03T19:12:01Z',
		outcome: 'accepted',
		...fields
	};
}

describe('an attempt', () => {
	test('a redelivery is another attempt of the same id', () => {
		const first = delivery();
		const again = delivery({ receivedAt: '2026-03-03T19:14:30Z' });
		expect(deliveryKey(first)).not.toBe(deliveryKey(again));
		expect(findDelivery([first, again], deliveryRef(again))).toBe(again);
		expect(findDelivery([first], { kind: 'event', id: deliveryKey(first) })).toBeUndefined();
	});

	test('names its source and event, and what sillage did with it', () => {
		expect(deliveryLabel(delivery())).toBe('GitHub pull_request');
		expect(deliveryLabel(delivery({ source: 'vikunja', event: undefined }))).toBe(
			'Vikunja delivery (event unknown)'
		);
		expect(outcomeText(delivery({ outcome: 'failed', detail: 'the queue is full' }))).toBe(
			'failed: sillage could not ask for every read (the queue is full)'
		);
	});
});

describe('deliveries around a state change', () => {
	const at = Date.parse('2026-03-03T19:12:00Z');

	test('are those received within half an hour, either way, in order', () => {
		const before = delivery({ id: 'before', receivedAt: '2026-03-03T18:42:00Z' });
		const tooEarly = delivery({ id: 'early', receivedAt: '2026-03-03T18:41:59Z' });
		const after = delivery({ id: 'after', receivedAt: '2026-03-03T19:42:00Z' });
		const tooLate = delivery({ id: 'late', receivedAt: '2026-03-03T19:42:01Z' });
		expect(deliveriesAround([tooEarly, before, after, tooLate], at).map((d) => d.id)).toEqual([
			'before',
			'after'
		]);
	});

	test('only a state change lists them', () => {
		expect(isStateChange(event({ kind: 'pr.merged' }))).toBe(true);
		expect(isStateChange(event({ kind: 'task.bucket_observed' }))).toBe(true);
		expect(isStateChange(event({ kind: 'comment.created' }))).toBe(false);
	});
});
