import { describe, expect, test } from 'vitest';
import type { Delivery } from '../decodeApi';
import { DELIVERY_MARKS_MAX_SPAN_MS, DELIVERY_ROW_ID, deliveryRow } from './deliveries';

const DELIVERY: Delivery = {
	source: 'github',
	instance: 'github.example.com',
	id: 'd-1',
	event: 'pull_request',
	receivedAt: '2026-03-03T19:12:01Z',
	outcome: 'accepted'
};

describe('the Sillage lane', () => {
	test('has no row without deliveries', () => {
		expect(deliveryRow([], DELIVERY_MARKS_MAX_SPAN_MS)).toBeUndefined();
	});

	test('draws the deliveries in a narrow enough window', () => {
		const row = deliveryRow([DELIVERY], DELIVERY_MARKS_MAX_SPAN_MS);
		expect(row).toMatchObject({ id: DELIVERY_ROW_ID, section: 'sillage', deliveries: [DELIVERY] });
		expect(row?.note).toBeUndefined();
	});

	test('only counts them in a wider one', () => {
		const row = deliveryRow([DELIVERY, DELIVERY], DELIVERY_MARKS_MAX_SPAN_MS + 1);
		expect(row?.deliveries).toEqual([]);
		expect(row?.note).toBe('2 deliveries · zoom in to see them');
	});
});
