import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import type { Deliveries } from '../decodeApi';
import type { Load } from '../load.svelte';
import { Selection } from '../selection.svelte';
import NearbyDeliveries from './NearbyDeliveries.svelte';

const AT = Date.parse('2026-03-03T19:12:00Z');
const ROOT = { task: { instance: 'vikunja.example.com', id: 102 } };

function show(deliveries: Load<Deliveries>, selection = new Selection()) {
	render(NearbyDeliveries, { deliveries, at: AT, selection, timeZone: 'UTC' });
	return selection;
}

test('lists the deliveries received nearby, and selects one', async () => {
	const selection = show({
		state: 'loaded',
		value: {
			root: ROOT,
			deliveries: [
				{
					source: 'github',
					instance: 'github.example.com',
					id: 'far',
					event: 'pull_request',
					receivedAt: '2026-03-03T18:00:00Z',
					outcome: 'accepted'
				},
				{
					source: 'github',
					instance: 'github.example.com',
					id: 'near',
					event: 'pull_request',
					receivedAt: '2026-03-03T19:12:01Z',
					outcome: 'failed'
				}
			],
			truncated: true
		}
	});
	const items = page.getByRole('listitem');
	await expect.element(items).toHaveTextContent(/GitHub pull_request, failed; not read since/);
	await page.getByRole('button', { name: 'GitHub pull_request' }).click();
	expect(selection.current).toEqual({
		kind: 'delivery',
		id: 'github/github.example.com/near@2026-03-03T19:12:01Z'
	});
	await expect.element(page.getByText(/older ones are not listed/)).toBeVisible();
});

test('none nearby is unknown, not a failure', async () => {
	show({ state: 'loaded', value: { root: ROOT, deliveries: [] } });
	await expect.element(page.getByTestId('no-delivery')).toHaveTextContent(/unknown, not a failure/);
});

test('says nothing when the page asked for no deliveries', async () => {
	show({ state: 'idle' });
	expect(document.querySelector('[data-nearby-deliveries]')).toBeNull();
});
