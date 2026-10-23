import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { API_BASE } from '../lib/api';
import { fakeServer, json, problem } from '../testing/fakeServer';
import SourceFreshness from './SourceFreshness.svelte';

const COVERAGE = `${API_BASE}/coverage`;

test("shows each source's last successful read", async () => {
	const server = fakeServer({
		[COVERAGE]: () =>
			json([
				{
					source: 'vikunja',
					instance: 'vikunja.example.com',
					lastSuccessfulRead: '2026-03-03T20:00:00Z'
				}
			])
	});
	await render(SourceFreshness, { api: server.api });
	await expect
		.element(page.getByRole('region', { name: 'Source freshness' }))
		.toHaveTextContent(
			'Vikunja vikunja.example.com Last read successfully at 2026-03-03 20:00:00 UTC.'
		);
});

test('a failed read says the freshness is unknown', async () => {
	const server = fakeServer({ [COVERAGE]: () => problem(500, 'Internal Server Error') });
	await render(SourceFreshness, { api: server.api });
	await expect.element(page.getByRole('alert')).toHaveTextContent('(500 Internal Server Error)');
	await expect
		.element(page.getByText("How recent the sources' records are is unknown."))
		.toBeVisible();
});
