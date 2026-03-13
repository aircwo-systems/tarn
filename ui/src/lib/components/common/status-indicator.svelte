<script lang="ts">
	import LedDot from './led-dot.svelte';

	let {
		status,
		text
	}: {
		status: 'ok' | 'error' | 'loading' | 'idle';
		text: string;
	} = $props();

	const ledColor = $derived(
		status === 'ok' ? 'green' as const :
		status === 'error' ? 'red' as const :
		status === 'loading' ? 'amber' as const :
		'gray' as const
	);
</script>

<div class="flex items-center gap-2 text-xs text-muted-foreground">
	<LedDot color={ledColor} size="md" />
	<span>{text}</span>
</div>
