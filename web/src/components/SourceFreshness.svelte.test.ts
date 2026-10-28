import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { API_BASE } from '../lib/api';
import { goldenResponse } from '../lib/testing/apiResponses';
import { fakeServer, json, problem } from '../testing/fakeServer';
import SourceFreshness from './SourceFreshness.svelte';

const COVERAGE = `${API_BASE}/coverage`;

test("shows each source's latest and stalest successful read", async () => {
	const server = fakeServer({ [COVERAGE]: () => json(goldenResponse(COVERAGE)) });
	await render(SourceFreshness, { api: server.api });
	await expect
		.element(page.getByRole('listitem').filter({ hasText: 'github.example.com' }))
		.toHaveTextContent(
			'GitHub github.example.com Read for 5 tasks. Last read successfully at 2026-03-15 10:00:00 UTC; the least recently read of them at 2026-03-02 15:00:00 UTC.'
		);
	expect(server.requests.map((r) => r.path)).toEqual([COVERAGE]);
});

test('a failed read says the freshness is unknown', async () => {
	const server = fakeServer({ [COVERAGE]: () => problem(500, 'Internal Server Error') });
	await render(SourceFreshness, { api: server.api });
	await expect.element(page.getByRole('alert')).toHaveTextContent('(500 Internal Server Error)');
	await expect
		.element(page.getByText("How recent the sources' records are is unknown."))
		.toBeVisible();
});
