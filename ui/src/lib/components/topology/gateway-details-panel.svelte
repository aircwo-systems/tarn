<script lang="ts">
	import { DownloadSimpleIcon, XIcon, CopyIcon, CheckIcon } from 'phosphor-svelte';
	import Badge from '$lib/components/ui/badge/badge.svelte';
	import type { GatewaySummary, RouteDetail } from '$lib/types';
	import { slugify, normalizeTemplate, parseTarget, buildPostmanCollection, buildPostmanEnvironment, downloadJSON } from '$lib/postman';

	let {
		gateway,
		onClose,
		showClose = true
	}: {
		gateway: GatewaySummary;
		onClose?: (() => void) | undefined;
		showClose?: boolean;
	} = $props();

	let urlCopied = $state(false);

	function copyInvokeUrl() {
		navigator.clipboard.writeText(gateway.invokeUrl).then(() => {
			urlCopied = true;
			setTimeout(() => { urlCopied = false; }, 1500);
		});
	}

	function routeParts(routeKey: string): { method: string; path: string } {
		if (routeKey === '$default') return { method: '$default', path: '/' };
		const firstSpace = routeKey.indexOf(' ');
		if (firstSpace === -1) return { method: 'ANY', path: routeKey };
		return { method: routeKey.slice(0, firstSpace), path: routeKey.slice(firstSpace + 1) || '/' };
	}

	function methodBadgeVariant(method: string): 'default' | 'secondary' | 'amber' | 'outline' | 'destructive' {
		const m = method.toUpperCase();
		if (m === 'GET') return 'default';
		if (m === 'POST' || m === 'PUT' || m === 'PATCH') return 'amber';
		if (m === 'DELETE') return 'destructive';
		if (m === '$DEFAULT') return 'secondary';
		return 'outline';
	}

	function integrationLabel(type: string): string {
		switch (type) {
			case 'AWS_PROXY': return 'Lambda Proxy';
			case 'AWS': return 'AWS';
			case 'HTTP_PROXY': return 'HTTP Proxy';
			case 'HTTP': return 'HTTP';
			case 'MOCK': return 'Mock';
			default: return type;
		}
	}

	function hasTemplates(detail: RouteDetail): boolean {
		return !!(detail.requestTemplates && Object.keys(detail.requestTemplates).length > 0);
	}

	function hasParams(detail: RouteDetail): boolean {
		return !!(detail.requestParameters && Object.keys(detail.requestParameters).length > 0);
	}

	function downloadCollection() {
		downloadJSON(`${slugify(gateway.name)}.postman_collection.json`, buildPostmanCollection(gateway));
	}

	function downloadEnvironment() {
		downloadJSON(`${slugify(gateway.name)}.postman_environment.json`, buildPostmanEnvironment(gateway));
	}
</script>

<section class="flex flex-col rounded-lg border border-border bg-bg-raised overflow-hidden">

	<!-- Header -->
	<div class="flex items-center justify-between border-b border-border px-3 py-2 shrink-0">
		<div class="min-w-0">
			<div class="flex items-center gap-2 min-w-0">
				<p class="truncate text-sm font-semibold text-text">{gateway.name}</p>
				<Badge variant="secondary" class="shrink-0">{gateway.protocolType}</Badge>
				<Badge variant="outline" class="shrink-0 text-[10px] px-1 py-0 font-mono">
					{gateway.version}
				</Badge>
			</div>
			{#if gateway.defaultStage}
				<p class="mt-0.5 font-mono text-[10px] text-text-faint">stage: {gateway.defaultStage}</p>
			{/if}
		</div>
		{#if showClose}
			<button
				type="button"
				onclick={() => onClose?.()}
				class="ml-2 flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-text-muted hover:bg-bg-overlay hover:text-text"
				aria-label="Close gateway panel"
			>
				<XIcon size={14} />
			</button>
		{/if}
	</div>

	<!-- Invoke URL bar -->
	<div class="flex items-center gap-2 border-b border-border bg-bg px-3 py-2 shrink-0">
		<span class="font-mono text-[10px] uppercase tracking-wide text-text-faint shrink-0">Invoke</span>
		<code class="flex-1 truncate font-mono text-[11px] text-text-muted">{gateway.invokeUrl}</code>
		<button
			type="button"
			onclick={copyInvokeUrl}
			class="flex h-6 w-6 shrink-0 items-center justify-center rounded text-text-faint hover:text-text transition-colors"
			aria-label="Copy invoke URL"
			title={urlCopied ? 'Copied!' : 'Copy invoke URL'}
		>
			{#if urlCopied}
				<CheckIcon size={12} class="text-accent" />
			{:else}
				<CopyIcon size={12} />
			{/if}
		</button>
	</div>

	<!-- Routes & Integrations -->
	<div class="flex-1 overflow-y-auto px-3 py-3 space-y-2">
		<p class="text-[10px] font-mono uppercase tracking-wide text-text-faint">
			Routes &amp; Integrations
			<span class="ml-1 text-text-muted">{gateway.routeDetails?.length ?? gateway.routes}</span>
		</p>

		{#if gateway.routeDetails?.length}
			{#each gateway.routeDetails as detail (detail.routeKey)}
				{@const target = detail.integrationTarget ? parseTarget(detail.integrationTarget) : null}
				<div class="rounded-md border border-border bg-bg-overlay/60 overflow-hidden">

					<!-- Route row: method · path · integration type -->
					<div class="flex items-center gap-2 px-2.5 py-2">
						<Badge variant={methodBadgeVariant(detail.method ?? '')} class="shrink-0">
							{detail.method ?? '—'}
						</Badge>
						<span class="flex-1 truncate font-mono text-xs text-text">
							{detail.path ?? detail.routeKey}
						</span>
						{#if detail.integrationType}
							<span class="shrink-0 font-mono text-[10px] text-text-faint">
								{integrationLabel(detail.integrationType)}
							</span>
						{/if}
					</div>

					<!-- Integration target -->
					{#if target}
						<div class="flex items-center gap-2 border-t border-border/60 px-2.5 py-1.5">
							<span class="rounded border border-border bg-bg-raised px-1.5 py-0.5 font-mono text-[10px] text-text-muted shrink-0">
								{target.kind}
							</span>
							<span class="truncate font-mono text-[11px] text-text-muted">{target.name}</span>
						</div>
					{/if}

					<!-- Request templates (v1 AWS integrations) -->
					{#if hasTemplates(detail)}
						{#each Object.entries(detail.requestTemplates!) as [contentType, template] (contentType)}
							<div class="border-t border-border/60 overflow-hidden">
								<div class="flex items-center justify-between bg-bg px-2.5 py-1 border-b border-border/40">
									<span class="font-mono text-[10px] text-text-faint">{contentType}</span>
									<span class="font-mono text-[10px] tracking-widest text-text-faint/60">template</span>
								</div>
								<pre class="overflow-x-auto bg-bg p-2.5 font-mono text-[11px] leading-relaxed text-text-muted whitespace-pre">{normalizeTemplate(template)}</pre>
							</div>
						{/each}
					{/if}

					<!-- Request parameters (v2 integrations) -->
					{#if hasParams(detail)}
						<div class="border-t border-border/60 overflow-hidden">
							<div class="bg-bg px-2.5 py-1 border-b border-border/40">
								<span class="font-mono text-[10px] text-text-faint">parameters</span>
							</div>
							<div class="bg-bg p-2.5 space-y-1">
								{#each Object.entries(detail.requestParameters!) as [key, value] (key)}
									<div class="flex items-baseline gap-2 font-mono text-[11px]">
										<span class="text-text-faint shrink-0">{key}</span>
										<span class="text-text-muted break-all">{value}</span>
									</div>
								{/each}
							</div>
						</div>
					{/if}

				</div>
			{/each}

		{:else if gateway.routeKeys?.length}
			<!-- Fallback when routeDetails not yet populated -->
			{#each gateway.routeKeys as routeKey (routeKey)}
				{@const route = routeParts(routeKey)}
				<div class="flex items-center gap-2 rounded-md border border-border bg-bg-overlay/60 px-2.5 py-2">
					<Badge variant={methodBadgeVariant(route.method)} class="shrink-0">{route.method}</Badge>
					<span class="font-mono text-xs text-text">{route.path}</span>
				</div>
			{/each}

		{:else}
			<p class="py-2 text-xs text-text-faint">No routes configured for this gateway.</p>
		{/if}
	</div>

	<!-- Attributes -->
	<div class="border-t border-border px-3 py-3 shrink-0">
		<p class="mb-2 text-[10px] font-mono uppercase tracking-wide text-text-faint">Attributes</p>
		<div class="space-y-1.5 text-xs">
			<div class="grid grid-cols-[5.5rem_1fr] gap-2">
				<span class="text-text-faint">API ID</span>
				<span class="font-mono text-text break-all">{gateway.apiId}</span>
			</div>
			<div class="grid grid-cols-[5.5rem_1fr] gap-2">
				<span class="text-text-faint">Endpoint</span>
				<span class="font-mono text-text break-all">{gateway.apiEndpoint || gateway.invokeUrl}</span>
			</div>
			<div class="grid grid-cols-[5.5rem_1fr] gap-2">
				<span class="text-text-faint">Routes</span>
				<span class="font-mono text-text">{gateway.routes}</span>
			</div>
			<div class="grid grid-cols-[5.5rem_1fr] gap-2">
				<span class="text-text-faint">Integrations</span>
				<span class="font-mono text-text">{gateway.integrations}</span>
			</div>
			<div class="grid grid-cols-[5.5rem_1fr] gap-2">
				<span class="text-text-faint">Stages</span>
				<span class="font-mono text-text">{gateway.stages}</span>
			</div>
			{#if gateway.description}
				<div class="grid grid-cols-[5.5rem_1fr] gap-2">
					<span class="text-text-faint">Description</span>
					<span class="text-text break-words">{gateway.description}</span>
				</div>
			{/if}
			<div class="grid grid-cols-[5.5rem_1fr] gap-2">
				<span class="text-text-faint">ARN</span>
				<span class="font-mono text-[11px] text-text-faint break-all">{gateway.arn}</span>
			</div>
		</div>
	</div>

	<!-- Postman Export -->
	<div class="border-t border-border px-3 py-3 shrink-0">
		<div class="flex items-center justify-between gap-2">
			<div>
				<p class="text-[10px] font-mono uppercase tracking-wide text-text-faint">Postman Export</p>
				<p class="mt-0.5 text-[11px] text-text-faint">Use the Postman Desktop Agent for local requests. Run Chaos Probe to enrich with examples.</p>
			</div>
			<div class="flex gap-2 shrink-0">
				<button
					type="button"
					onclick={downloadCollection}
					class="inline-flex items-center gap-1 rounded-md border border-accent-strong bg-accent-muted px-3 py-1.5 text-xs text-accent hover:bg-accent/20"
				>
					<DownloadSimpleIcon size={12} />
					Collection
				</button>
				<button
					type="button"
					onclick={downloadEnvironment}
					class="inline-flex items-center gap-1 rounded-md border border-border bg-bg-raised px-3 py-1.5 text-xs text-text hover:bg-bg-overlay"
				>
					<DownloadSimpleIcon size={12} />
					Env
				</button>
			</div>
		</div>
	</div>

</section>
