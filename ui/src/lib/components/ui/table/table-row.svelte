<script lang="ts">
	import { cn, type WithElementRef } from "$lib/lib/utils.js";
	import type { HTMLAttributes } from "svelte/elements";

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLTableRowElement>> = $props();
</script>

<tr
	bind:this={ref}
	data-slot="table-row"
	class={cn(
		"border-b border-border/40 transition-colors hover:bg-white/[0.03] dark:hover:bg-white/[0.04] data-[state=selected]:bg-muted/50",
		className
	)}
	{...restProps}
>
	{@render children?.()}
</tr>
