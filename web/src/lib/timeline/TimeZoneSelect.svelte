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
</style>
