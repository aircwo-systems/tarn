<script lang="ts">
	import { TrayIcon } from 'phosphor-svelte';

	let {
		icon: Icon = TrayIcon,
		message = 'No items yet.'
	}: {
		icon?: any;
		message?: string;
	} = $props();
</script>

<div class="flex flex-col items-center justify-center gap-2 py-10 text-text-faint">
	<Icon size={28} strokeWidth={1} />
	<p class="text-sm">{message}</p>
</div>
