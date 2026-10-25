<script lang="ts">
	import { zoneName, type Zone } from './time';

	let { zone = $bindable() }: { zone: Zone } = $props();
</script>

<label>
	Time zone
	<select bind:value={zone}>
		<option value="local">Local ({zoneName('local')})</option>
		<option value="utc">UTC</option>
	</select>
</label>

<style>
	label {
		display: inline-flex;
		gap: 0.25rem;
		align-items: center;
	}
	select {
		font: inherit;
		color: inherit;
		background: inherit;
		border: 1px solid currentColor;
		border-radius: 4px;
		padding: 0.2rem 0.5rem;
	}
</style>
