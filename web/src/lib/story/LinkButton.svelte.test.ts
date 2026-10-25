import { createRawSnippet } from 'svelte';
import { expect, test, vi } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';
import LinkButton from './LinkButton.svelte';

test('a plain button that draws its text as a link and passes the click on', async () => {
	const onclick = vi.fn();
	const children = createRawSnippet(() => ({ render: () => '<span>the run</span>' }));
	render(LinkButton, { onclick, children });
	const button = page.getByRole('button', { name: 'the run' });
	await expect.element(button).toHaveAttribute('type', 'button');
	await expect.element(button).toHaveStyle({ textDecorationLine: 'underline' });
	await button.click();
	expect(onclick).toHaveBeenCalledExactlyOnceWith(expect.any(MouseEvent));
});
