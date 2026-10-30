import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import type { Delivery } from '../decodeApi';
import DeliveryDetail from './DeliveryDetail.svelte';

const DELIVERY: Delivery = {
	source: 'vikunja',
	instance: 'vikunja.example.com',
	id: 'abc123',
	event: 'task.updated',
	receivedAt: '2026-03-03T19:25:04Z',
	outcome: 'accepted'
};

test('a delivery read since names its next read', async () => {
	render(DeliveryDetail, {
		delivery: { ...DELIVERY, nextRead: '2026-03-03T19:30:00Z' },
		timeZone: 'UTC'
	});
	await expect
		.element(page.getByRole('heading', { level: 3 }))
		.toHaveTextContent('Webhook delivery: Vikunja task.updated');
	await expect
		.element(page.getByTestId('next-read'))
		.toHaveTextContent(/^2026-03-03 19:30:00 UTC: the first read of the task from Vikunja/);
	await expect.element(page.getByText(/linked to nothing/)).toBeVisible();
});

test('a delivery not read since, whose payload was unreadable, says so', async () => {
	render(DeliveryDetail, {
		delivery: { ...DELIVERY, event: undefined, outcome: 'rejected', detail: 'not JSON' },
		timeZone: 'UTC'
	});
	await expect.element(page.getByTestId('next-read')).toHaveTextContent(/^none yet/);
	await expect
		.element(page.getByTestId('delivery-outcome'))
		.toHaveTextContent('rejected: sillage could not read the payload (not JSON)');
	await expect.element(page.getByText(/sillage could not read the payload$/)).toBeVisible();
});
