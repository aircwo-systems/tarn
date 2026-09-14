<script lang="ts">
	import { cn, type WithElementRef } from "$lib/lib/utils.js";
	import type { HTMLAttributes } from "svelte/elements";

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: WithElementRef<HTMLAttributes<HTMLTableSectionElement>> = $props();
</script>

<thead
	bind:this={ref}
	data-slot="table-header"
	class={cn("border-b border-border/70 bg-card/40 backdrop-blur-sm [&_tr]:border-b-0", className)}
	{...restProps}
>
	{@render children?.()}
</thead>
