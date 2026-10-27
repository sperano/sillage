<!-- "Paste a link": opens the task story a Vikunja, Kelos or GitHub link belongs to. -->
<script lang="ts">
	import type { Router } from '../lib/router.svelte';
	import { URL_PARAM, openHref } from '../lib/routes';

	let { router }: { router: Router } = $props();
	const uid = $props.id();

	// A blank link (spaces only) still opens /open, which says there is none.
	function submit(event: SubmitEvent & { currentTarget: HTMLFormElement }) {
		event.preventDefault();
		const link = new FormData(event.currentTarget).get(URL_PARAM);
		router.navigate(openHref(typeof link === 'string' ? link.trim() : ''));
	}
</script>

<section aria-labelledby="{uid}-heading">
	<h2 id="{uid}-heading">Open a link</h2>
	<form onsubmit={submit}>
		<label>
			Link
			<input type="text" inputmode="url" name={URL_PARAM} required aria-describedby="{uid}-hint" />
		</label>
		<button type="submit">Open</button>
	</form>
	<p id="{uid}-hint">A Vikunja task, a Kelos Task or a GitHub pull request.</p>
</section>
