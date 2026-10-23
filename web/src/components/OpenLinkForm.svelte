<!-- "Paste a link": opens the story a Vikunja, Kelos, GitHub or wiki link belongs to. -->
<script lang="ts">
	import type { Router } from '../lib/router.svelte';
	import { URL_PARAM, openHref } from '../lib/routes';

	let { router }: { router: Router } = $props();

	function submit(event: SubmitEvent & { currentTarget: HTMLFormElement }) {
		event.preventDefault();
		const link = new FormData(event.currentTarget).get(URL_PARAM);
		if (typeof link === 'string' && link.trim() !== '') router.navigate(openHref(link.trim()));
	}
</script>

<section aria-labelledby="open-heading">
	<h2 id="open-heading">Open a link</h2>
	<form onsubmit={submit}>
		<label>
			Link
			<input type="text" inputmode="url" name={URL_PARAM} required aria-describedby="open-hint" />
		</label>
		<button type="submit">Open</button>
	</form>
	<p id="open-hint">A Vikunja task, a Kelos Task, a GitHub pull request or a wiki page.</p>
</section>
