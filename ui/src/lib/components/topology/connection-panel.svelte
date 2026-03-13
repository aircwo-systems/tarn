<script lang="ts">
	import type { InfraConnection, InfraProbe } from '$lib/types';

	let {
		region,
		accountId,
		endpoint,
		infrastructure = [],
		connections = []
	}: {
		region: string;
		accountId: string;
		endpoint: string;
		infrastructure?: InfraProbe[];
		connections?: InfraConnection[];
	} = $props();

	function probeId(probe: InfraProbe): string {
		return `${probe.kind}-${probe.host}-${probe.port}`;
	}

	function connectionsForProbe(probe: InfraProbe): InfraConnection[] {
		return connections.filter((connection) => connection.targetId === probeId(probe));
	}
</script>

<div class="grid gap-2 text-xs">
	<p class="text-[10px] font-mono uppercase tracking-wider text-muted-foreground/70">Connection</p>
	<dl class="grid gap-1.5">
		<div class="flex items-baseline justify-between gap-2">
			<dt class="text-muted-foreground/70 font-mono uppercase text-[10px] tracking-wider">Region</dt>
			<dd class="font-mono text-muted-foreground truncate">{region}</dd>
		</div>
		<div class="flex items-baseline justify-between gap-2">
			<dt class="text-muted-foreground/70 font-mono uppercase text-[10px] tracking-wider">Account</dt>
			<dd class="font-mono text-muted-foreground truncate">{accountId}</dd>
		</div>
		<div class="flex items-baseline justify-between gap-2">
			<dt class="text-muted-foreground/70 font-mono uppercase text-[10px] tracking-wider">API</dt>
			<dd class="font-mono text-muted-foreground truncate text-[11px]">{endpoint}</dd>
		</div>
	</dl>

	{#if infrastructure.length > 0}
			<p class="text-[10px] font-mono uppercase tracking-wider text-muted-foreground/70 mt-1">Infrastructure</p>
			<ul class="grid gap-1">
				{#each infrastructure as probe (probe.kind + probe.host + probe.port)}
					{@const evidenceConnections = connectionsForProbe(probe)}
					<li class="grid gap-1 text-[11px]">
						<div class="flex items-center gap-2">
						<span
							class="inline-block h-1.5 w-1.5 rounded-full shrink-0"
							class:bg-accent={probe.status === 'connected'}
							class:bg-red={probe.status !== 'connected'}
						></span>
						<span class="font-mono text-muted-foreground truncate flex-1">{probe.name}</span>
						{#if probe.port > 0}
							<span class="font-mono text-muted-foreground/70 text-[10px]">:{probe.port}</span>
						{/if}
						{#if probe.status === 'connected'}
							<span class="font-mono text-muted-foreground/70 text-[10px]">{Math.round(probe.latencyMs)}ms</span>
						{:else}
							<span class="font-mono text-muted-foreground/70 text-[10px]">&mdash;</span>
						{/if}
					</div>

						{#if evidenceConnections.length > 0}
							<div class="ml-3.5 flex flex-wrap items-center gap-1.5">
							<span class="text-[9px] font-mono uppercase tracking-wider text-muted-foreground/70">Evidence</span>
							{#each evidenceConnections as connection (`${connection.sourceFunction}-${connection.source}`)}
								<span
									class="inline-flex items-center gap-1 rounded-full border border-border bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground"
									title={`${connection.sourceFunction} matched via ${connection.source}`}
								>
									<span class="font-mono text-foreground">{connection.sourceFunction}</span>
									<span class="text-muted-foreground/70">{connection.source}</span>
								</span>
							{/each}
						</div>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
</div>
