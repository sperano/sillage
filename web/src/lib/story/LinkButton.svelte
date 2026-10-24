<script lang="ts">
	import type { Snippet } from 'svelte';

	// A button drawn as a link: what a detail selects another record with.

	let { onclick, children }: { onclick: (event: MouseEvent) => void; children: Snippet } = $props();
</script>

<button type="button" class="link" {onclick}>{@render children()}</button>

<style>
	.link {
		padding: 0;
		border: none;
		background: none;
		color: LinkText;
		font: inherit;
		text-decoration: underline;
		cursor: pointer;
	}
</style>
