<script lang="ts">
	import { cn } from '$lib/utils';

	let {
		class: className,
		children,
		...rest
	}: { class?: string; children?: import('svelte').Snippet; [key: string]: any } = $props();
</script>

<div class="w-full overflow-auto">
	<table class={cn('w-full caption-bottom text-sm', className)} {...rest}>
		{@render children?.()}
	</table>
</div>
