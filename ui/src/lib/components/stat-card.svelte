<script lang="ts">
	import { Card } from '$lib/components/ui/card';
	import { cn } from '$lib/utils';

	let {
		label,
		value,
		subtitle,
		accentColor = 'accent',
		class: className
	}: {
		label: string;
		value: string | number;
		subtitle: string;
		accentColor?: 'accent' | 'amber' | 'blue' | 'default';
		class?: string;
	} = $props();

	const borderColors = {
		accent: 'border-t-accent',
		amber: 'border-t-amber',
		blue: 'border-t-blue',
		default: 'border-t-border-strong'
	};
</script>

<Card class={cn('border-t-2 px-3 py-3', borderColors[accentColor], className)}>
	<p class="text-[10px] font-mono uppercase tracking-wider text-text-muted">{label}</p>
	<p class="text-2xl font-semibold tracking-tight text-text mt-1">{value}</p>
	<p class="text-xs text-text-faint mt-0.5">{subtitle}</p>
</Card>
