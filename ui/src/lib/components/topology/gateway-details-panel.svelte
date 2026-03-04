<script lang="ts">
	import { DownloadSimpleIcon, XIcon } from 'phosphor-svelte';
	import Badge from '$lib/components/ui/badge/badge.svelte';
	import type { GatewaySummary } from '$lib/types';

	let {
		gateway,
		onClose,
		showClose = true
	}: {
		gateway: GatewaySummary;
		onClose?: (() => void) | undefined;
		showClose?: boolean;
	} = $props();

	function routeParts(routeKey: string): { method: string; path: string } {
		if (routeKey === '$default') {
			return { method: '$default', path: '/' };
		}

		const firstSpace = routeKey.indexOf(' ');
		if (firstSpace === -1) {
			return { method: 'ANY', path: routeKey };
		}

		return {
			method: routeKey.slice(0, firstSpace),
			path: routeKey.slice(firstSpace + 1) || '/'
		};
	}

	function methodBadgeVariant(method: string): 'default' | 'secondary' | 'amber' | 'outline' | 'destructive' {
		const normalized = method.toUpperCase();
		if (normalized === 'GET') return 'default';
		if (normalized === 'POST' || normalized === 'PUT' || normalized === 'PATCH') return 'amber';
		if (normalized === 'DELETE') return 'destructive';
		if (normalized === '$DEFAULT') return 'secondary';
		return 'outline';
	}

	function normalizeBaseUrl(raw: string): string {
		try {
			const parsed = new URL(raw);
			if (parsed.hostname === '0.0.0.0' || parsed.hostname === '::' || parsed.hostname === '[::]') {
				parsed.hostname = '127.0.0.1';
			}
			return parsed.origin;
		} catch {
			return raw;
		}
	}

	function slugify(value: string): string {
		return value
			.toLowerCase()
			.replace(/[^a-z0-9]+/g, '-')
			.replace(/^-+|-+$/g, '') || 'gateway';
	}

	function postmanPath(path: string): string {
		if (!path || path === '/') return '';
		return path.replace(/\{([a-zA-Z0-9_]+)\+?\}/g, '{{$1}}');
	}

	function routeVariableNames(path: string): string[] {
		const matches = path.matchAll(/\{([a-zA-Z0-9_]+)\+?\}/g);
		const names = new Set<string>();
		for (const match of matches) {
			if (match[1]) {
				names.add(match[1]);
			}
		}
		return Array.from(names);
	}

	function baseUrl(gateway: GatewaySummary): string {
		return normalizeBaseUrl(gateway.invokeUrl);
	}

	function collectionVariables(gateway: GatewaySummary): Array<{ key: string; value: string; type?: string }> {
		const vars = new Map<string, { key: string; value: string; type?: string }>();
		vars.set('baseUrl', { key: 'baseUrl', value: baseUrl(gateway), type: 'string' });
		vars.set('apiId', { key: 'apiId', value: gateway.apiId, type: 'string' });
		vars.set('stage', { key: 'stage', value: gateway.defaultStage, type: 'string' });
		vars.set('invokeBase', {
			key: 'invokeBase',
			value: '{{baseUrl}}/_apigateway/{{apiId}}/{{stage}}',
			type: 'string'
		});

		for (const routeKey of gateway.routeKeys ?? []) {
			const route = routeParts(routeKey);
			for (const variableName of routeVariableNames(route.path)) {
				if (!vars.has(variableName)) {
					vars.set(variableName, { key: variableName, value: '', type: 'string' });
				}
			}
			if (route.method === '$default' && !vars.has('defaultPath')) {
				vars.set('defaultPath', { key: 'defaultPath', value: '', type: 'string' });
			}
		}

		return Array.from(vars.values());
	}

	function collectionItems(gateway: GatewaySummary) {
		return (gateway.routeKeys ?? []).map((routeKey) => {
			const route = routeParts(routeKey);
			const exportedMethod = route.method === 'ANY' || route.method === '$default' ? 'GET' : route.method;
			const rawPath =
				route.method === '$default'
					? '{{invokeBase}}/{{defaultPath}}'
					: `{{invokeBase}}${postmanPath(route.path)}`;

			return {
				name: routeKey,
				request: {
					method: exportedMethod,
					header:
						exportedMethod === 'GET' || exportedMethod === 'HEAD'
							? []
							: [{ key: 'Content-Type', value: 'application/json', type: 'text' }],
					url: rawPath,
					description:
						route.method === 'ANY'
							? `Generated from ${routeKey}. Exported as GET placeholder because Postman requests need a concrete method.`
							: route.method === '$default'
								? 'Generated from $default route. Set {{defaultPath}} before sending.'
								: `Generated from ${routeKey}.`
				},
				response: []
			};
		});
	}

	function buildPostmanCollection(gateway: GatewaySummary) {
		return {
			info: {
				name: `${gateway.name} (OpenStack API Gateway)`,
				description: `Generated from OpenStack API Gateway ${gateway.apiId}.`,
				schema: 'https://schema.getpostman.com/json/collection/v2.1.0/collection.json'
			},
			variable: collectionVariables(gateway),
			item: collectionItems(gateway)
		};
	}

	function buildPostmanEnvironment(gateway: GatewaySummary) {
		return {
			name: `${gateway.name} (OpenStack Local)`,
			values: collectionVariables(gateway).map((variable) => ({
				key: variable.key,
				value: variable.value,
				enabled: true,
				type: variable.type ?? 'text'
			})),
			_postman_variable_scope: 'environment',
			_postman_exported_at: new Date().toISOString(),
			_postman_exported_using: 'OpenStack dashboard'
		};
	}

	function downloadJSON(filename: string, payload: unknown) {
		const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
		const href = URL.createObjectURL(blob);
		const link = document.createElement('a');
		link.href = href;
		link.download = filename;
		document.body.appendChild(link);
		link.click();
		link.remove();
		URL.revokeObjectURL(href);
	}

	function downloadCollection() {
		downloadJSON(`${slugify(gateway.name)}.postman_collection.json`, buildPostmanCollection(gateway));
	}

	function downloadEnvironment() {
		downloadJSON(`${slugify(gateway.name)}.postman_environment.json`, buildPostmanEnvironment(gateway));
	}
</script>

<section class="rounded-lg border border-border bg-bg-raised">
	<div class="flex items-center justify-between border-b border-border px-3 py-2">
		<div class="min-w-0">
			<p class="truncate text-sm font-semibold text-text">{gateway.name}</p>
			<p class="text-[10px] font-mono uppercase tracking-wide text-text-faint">{gateway.protocolType} API Gateway</p>
		</div>
		{#if showClose}
			<button
				type="button"
				onclick={() => onClose?.()}
				class="flex h-7 w-7 items-center justify-center rounded-md text-text-muted hover:bg-bg-overlay hover:text-text"
				aria-label="Close gateway panel"
			>
				<XIcon size={14} />
			</button>
		{/if}
	</div>

	<div class="space-y-4 px-3 py-3">
		<div class="rounded-md border border-border bg-bg-overlay/70 p-3">
			<div class="flex flex-wrap items-center justify-between gap-2">
				<div>
					<p class="text-[10px] font-mono uppercase tracking-wide text-text-faint">Postman Export</p>
					<p class="mt-1 text-[11px] text-text-faint">
						Generate a Postman collection and environment for this gateway. Use the Postman Desktop Agent for local requests.
					</p>
				</div>
				<div class="flex flex-wrap gap-2">
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
						Environment
					</button>
				</div>
			</div>
		</div>

		<div class="rounded-md border border-border bg-bg-overlay/70 p-3">
			<p class="mb-2 text-[10px] font-mono uppercase tracking-wide text-text-faint">Endpoints</p>
			<div class="space-y-2">
				{#if gateway.routeKeys?.length}
					{#each gateway.routeKeys as routeKey}
						{@const route = routeParts(routeKey)}
						<div class="rounded-md border border-border bg-bg-raised/70 p-2">
							<div class="flex items-center gap-2">
								<Badge variant={methodBadgeVariant(route.method)}>{route.method}</Badge>
								<span class="font-mono text-xs text-text break-all">{route.path}</span>
							</div>
							<p class="mt-1 break-all font-mono text-[11px] text-text-faint">
								{gateway.invokeUrl}{route.path === '/' ? '' : route.path}
							</p>
							<p class="mt-1 text-[11px] text-text-faint">Route target mapping not available in dashboard yet.</p>
						</div>
					{/each}
				{:else}
					<div class="rounded-md border border-border bg-bg-raised/70 p-2 text-xs text-text-faint">
						No routes are currently exposed for this gateway.
					</div>
				{/if}
			</div>
		</div>

		<div class="rounded-md border border-border bg-bg-overlay/70 p-3">
			<p class="mb-2 text-[10px] font-mono uppercase tracking-wide text-text-faint">Attributes</p>
			<div class="space-y-2 text-xs">
				<div class="grid grid-cols-[6.5rem_1fr] gap-2">
					<span class="text-text-faint">API ID</span>
					<span class="font-mono text-text break-all">{gateway.apiId}</span>
				</div>
				<div class="grid grid-cols-[6.5rem_1fr] gap-2">
					<span class="text-text-faint">Invoke URL</span>
					<span class="font-mono text-text break-all">{gateway.invokeUrl}</span>
				</div>
				<div class="grid grid-cols-[6.5rem_1fr] gap-2">
					<span class="text-text-faint">API Endpoint</span>
					<span class="font-mono text-text break-all">{gateway.apiEndpoint}</span>
				</div>
				<div class="grid grid-cols-[6.5rem_1fr] gap-2">
					<span class="text-text-faint">Default Stage</span>
					<span class="font-mono text-text break-all">{gateway.defaultStage}</span>
				</div>
				<div class="grid grid-cols-[6.5rem_1fr] gap-2">
					<span class="text-text-faint">Routes</span>
					<span class="font-mono text-text">{gateway.routes}</span>
				</div>
				<div class="grid grid-cols-[6.5rem_1fr] gap-2">
					<span class="text-text-faint">Integrations</span>
					<span class="font-mono text-text">{gateway.integrations}</span>
				</div>
				<div class="grid grid-cols-[6.5rem_1fr] gap-2">
					<span class="text-text-faint">Stages</span>
					<span class="font-mono text-text">{gateway.stages}</span>
				</div>
				{#if gateway.description}
					<div class="grid grid-cols-[6.5rem_1fr] gap-2">
						<span class="text-text-faint">Description</span>
						<span class="text-text break-words">{gateway.description}</span>
					</div>
				{/if}
				<div class="grid grid-cols-[6.5rem_1fr] gap-2">
					<span class="text-text-faint">ARN</span>
					<span class="font-mono text-text break-all">{gateway.arn}</span>
				</div>
			</div>
			<p class="mt-3 text-[11px] text-text-faint">
				Integration and request/response mapping details are not surfaced yet. They will appear here when available.
			</p>
		</div>
	</div>
</section>
