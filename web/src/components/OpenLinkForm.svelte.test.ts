import { expect, test } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import { memoryHistory } from '../lib/history';
import { Router } from '../lib/router.svelte';
import { openHref } from '../lib/routes';
import OpenLinkForm from './OpenLinkForm.svelte';

test('opening a pasted link goes to /open with it', async () => {
	const history = memoryHistory('/');
	await render(OpenLinkForm, { router: new Router(history) });
	const link = 'https://github.example.com/example-org/widgets/pull/7';
	await userEvent.fill(page.getByRole('textbox', { name: 'Link' }), ` ${link} `);
	await page.getByRole('button', { name: 'Open' }).click();
	expect(history.entries()).toEqual(['/', openHref(link)]);
});

test('says which links it takes', async () => {
	await render(OpenLinkForm, { router: new Router(memoryHistory('/')) });
	await expect
		.element(page.getByRole('textbox', { name: 'Link' }))
		.toHaveAccessibleDescription('A Vikunja task, a Kelos Task or a GitHub pull request.');
});
