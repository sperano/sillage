import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import App from './App.svelte';

test('renders the placeholder page', async () => {
	render(App);
	await expect.element(page.getByRole('heading', { level: 1 })).toHaveTextContent('Sillage');
});
