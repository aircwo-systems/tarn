<script lang="ts">
	import { SquaresFour, GlobeHemisphereWest, Lightning, ChatCircle, Key, Scroll, SidebarSimple, ArrowsClockwise, Gear, X } from 'phosphor-svelte';
	import NavRailItem from './nav-rail-item.svelte';
	import ThemeToggle from './theme-toggle.svelte';
	import StatusIndicator from './status-indicator.svelte';
	import ConnectionPanel from './connection-panel.svelte';
	import LedDot from './led-dot.svelte';
	import { Separator } from '$lib/components/ui/separator';
	import { getDashboard, getUISettings, refresh, setPersistenceEnabled, setPollingIntervalSeconds, setThemeMode, type ThemeMode } from '$lib/state.svelte';

	let {
		activeTab = 'overview',
		onTabChange
	}: {
		activeTab?: string;
		onTabChange?: (tab: string) => void;
	} = $props();

	const dashboard = getDashboard();
	const uiSettings = getUISettings();

	let collapsed = $state(false);
	let settingsOpen = $state(false);
	let pollingIntervalDraft = $state(uiSettings.pollingIntervalSeconds);
	let themeModeDraft = $state<ThemeMode>(uiSettings.themeMode);
	let persistenceDraft = $state(uiSettings.persistenceEnabled);

	if (typeof window !== 'undefined') {
		collapsed = localStorage.getItem('openstack-nav-collapsed') === 'true';
	}

	function toggleCollapsed() {
		collapsed = !collapsed;
		localStorage.setItem('openstack-nav-collapsed', String(collapsed));
	}

	const tabs = [
		{ id: 'overview', label: 'Overview', icon: SquaresFour },
		{ id: 'gateways', label: 'Gateways', icon: GlobeHemisphereWest },
		{ id: 'functions', label: 'Functions', icon: Lightning },
		{ id: 'queues', label: 'Queues', icon: ChatCircle },
		{ id: 'secrets', label: 'Secrets', icon: Key },
		{ id: 'logs', label: 'Logs', icon: Scroll }
	];

	const connectionStatus = $derived(
		dashboard.error ? 'error' as const :
		dashboard.loading ? 'loading' as const :
		dashboard.data ? 'ok' as const :
		'idle' as const
	);

	const statusText = $derived(
		dashboard.error ? dashboard.error :
		dashboard.loading ? 'Connecting...' :
		dashboard.data?.status === 'running' ? `Connected · ${dashboard.lastRefresh}` :
		'Status unknown'
	);

	let refreshing = $state(false);
	async function handleRefresh() {
		refreshing = true;
		await refresh();
		refreshing = false;
	}

	function openSettings() {
		pollingIntervalDraft = uiSettings.pollingIntervalSeconds;
		themeModeDraft = uiSettings.themeMode;
		persistenceDraft = uiSettings.persistenceEnabled;
		settingsOpen = true;
	}

	function closeSettings() {
		settingsOpen = false;
	}

	function applySettings() {
		setPollingIntervalSeconds(pollingIntervalDraft);
		setThemeMode(themeModeDraft);
		setPersistenceEnabled(persistenceDraft);
		settingsOpen = false;
	}

	function handleWindowKeydown(event: KeyboardEvent) {
		if (event.key === 'Escape' && settingsOpen) {
			settingsOpen = false;
		}
	}
</script>

<svelte:window onkeydown={handleWindowKeydown} />

<aside
	class="hidden md:flex flex-col border-r border-border bg-bg-raised h-screen sticky top-0 transition-[width] duration-200 overflow-hidden shrink-0"
	style:width={collapsed ? '56px' : '200px'}
>
	<!-- Brand -->
	<div class="flex items-center gap-2.5 px-3 py-3 shrink-0">
		<div class="flex items-center justify-center h-7 w-7 rounded-md bg-accent/15 shrink-0">
			<LedDot color="green" size="md" />
		</div>
		{#if !collapsed}
			<div class="min-w-0">
				<p class="text-[10px] font-mono uppercase tracking-wider text-text-faint">OpenStack</p>
				<p class="text-sm font-semibold text-text truncate">Rack Console</p>
			</div>
		{/if}
	</div>

	<Separator />

	<!-- Nav items -->
	<nav class="flex flex-col gap-0.5 px-1.5 py-2 flex-1" aria-label="Dashboard sections">
		{#each tabs as tab}
			<NavRailItem
				icon={tab.icon}
				label={tab.label}
				active={activeTab === tab.id}
				{collapsed}
				onclick={() => onTabChange?.(tab.id)}
			/>
		{/each}
	</nav>

	<!-- Bottom section -->
	<div class="mt-auto flex flex-col gap-2 px-2 pb-2 shrink-0">
		{#if !collapsed}
			<Separator />
			<StatusIndicator status={connectionStatus} text={statusText} />

			{#if dashboard.data}
				<Separator />
				<ConnectionPanel
					region={dashboard.data.config.region}
					accountId={dashboard.data.config.accountId}
					endpoint={dashboard.data.config.endpoint}
					infrastructure={dashboard.data.infrastructure ?? []}
					connections={dashboard.data.connections ?? []}
				/>
			{/if}

			<button
				type="button"
				onclick={handleRefresh}
				disabled={refreshing || dashboard.loading}
				class="flex items-center justify-center gap-1.5 w-full h-7 rounded-md border border-accent-strong bg-accent-muted text-xs text-accent hover:bg-accent/20 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
			>
				<ArrowsClockwise size={12} class={refreshing ? 'animate-spin' : ''} />
				{refreshing ? 'Refreshing...' : 'Refresh'}
			</button>
		{/if}

		<Separator />
		<div class="flex items-center justify-between">
			<div class="flex items-center gap-1">
				<ThemeToggle />
				<button
					type="button"
					onclick={openSettings}
					class="flex items-center justify-center h-8 w-8 rounded-md text-text-muted hover:text-text hover:bg-bg-surface transition-colors"
					aria-label="Open UI settings"
				>
					<Gear size={15} />
				</button>
			</div>
			<button
				type="button"
				onclick={toggleCollapsed}
				class="flex items-center justify-center h-8 w-8 rounded-md text-text-muted hover:text-text hover:bg-bg-surface transition-colors"
				aria-label={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
			>
				{#if collapsed}
					<SidebarSimple size={15} />
				{:else}
					<SidebarSimple size={15} weight="fill" />
				{/if}
			</button>
		</div>
	</div>
</aside>

<!-- Mobile bottom tab bar -->
<nav class="fixed bottom-0 inset-x-0 z-50 flex md:hidden items-center justify-around border-t border-border bg-bg-raised/95 backdrop-blur-sm h-14 px-2" aria-label="Dashboard sections">
	{#each tabs as tab}
		{@const TabIcon = tab.icon}
		<button
			type="button"
			class="flex flex-col items-center gap-0.5 py-1 px-3 text-[10px] transition-colors {activeTab === tab.id ? 'text-accent' : 'text-text-muted'}"
			onclick={() => onTabChange?.(tab.id)}
			aria-current={activeTab === tab.id ? 'page' : undefined}
		>
			<TabIcon size={18} weight={activeTab === tab.id ? 'fill' : 'regular'} />
			{tab.label}
		</button>
	{/each}
</nav>

{#if settingsOpen}
	<div class="fixed inset-0 z-[70] bg-black/45" onclick={closeSettings} aria-hidden="true"></div>
	<div
		role="dialog"
		aria-modal="true"
		aria-label="UI Settings"
		class="fixed z-[75] left-1/2 top-1/2 w-[min(32rem,90vw)] -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-bg-raised shadow-xl"
	>
		<div class="flex items-center justify-between border-b border-border px-4 py-3">
			<h2 class="text-sm font-semibold text-text">UI Settings</h2>
			<button
				type="button"
				onclick={closeSettings}
				class="flex items-center justify-center h-7 w-7 rounded-md text-text-muted hover:text-text hover:bg-bg-surface transition-colors"
				aria-label="Close settings"
			>
				<X size={14} />
			</button>
		</div>

		<div class="space-y-4 px-4 py-4">
			<p class="text-xs text-text-faint">These preferences are saved in a browser cookie.</p>

			<div class="space-y-1.5">
				<label class="text-xs font-medium text-text" for="polling-interval">Polling Interval (seconds)</label>
				<input
					id="polling-interval"
					type="number"
					min="1"
					max="120"
					step="1"
					bind:value={pollingIntervalDraft}
					class="w-full rounded-md border border-border bg-bg-surface px-2.5 py-1.5 text-sm text-text outline-none focus:ring-1 focus:ring-accent"
				/>
			</div>

			<div class="space-y-1.5">
				<label class="text-xs font-medium text-text" for="theme-mode">Theme</label>
				<select
					id="theme-mode"
					bind:value={themeModeDraft}
					class="w-full rounded-md border border-border bg-bg-surface px-2.5 py-1.5 text-sm text-text outline-none focus:ring-1 focus:ring-accent"
				>
					<option value="system">System</option>
					<option value="light">Light</option>
					<option value="dark">Dark</option>
				</select>
			</div>

			<div class="rounded-md border border-border bg-bg-surface/70 p-3">
				<div class="flex items-start justify-between gap-3">
					<div class="min-w-0">
						<label class="text-xs font-medium text-text" for="persistence-enabled">Persistence</label>
						<p class="mt-1 text-[11px] leading-relaxed text-text-faint">
							Persist configuration over OpenStack sessions. Intended to allow for config to be saved and reused instead of building instance each time.
						</p>
					</div>
					<label class="relative inline-flex cursor-pointer items-center self-center">
						<input
							id="persistence-enabled"
							type="checkbox"
							bind:checked={persistenceDraft}
							class="peer sr-only"
						/>
						<span class="h-6 w-11 rounded-full border border-border bg-bg-surface transition-colors peer-checked:border-accent-strong peer-checked:bg-accent-muted"></span>
						<span class="pointer-events-none absolute left-0.5 top-0.5 h-5 w-5 rounded-full bg-white shadow-sm transition-transform peer-checked:translate-x-5 dark:bg-zinc-950"></span>
					</label>
				</div>
				<div class="mt-2 text-[11px] font-mono text-text-faint">
					{persistenceDraft ? 'true' : 'false'}
				</div>
			</div>

			<div class="rounded-md border border-border bg-bg-surface/70 p-3">
				<p class="mb-2 text-xs font-semibold uppercase tracking-wide text-text-faint">Instance Info</p>
				<div class="space-y-1.5 text-xs">
					<div class="grid grid-cols-[6.5rem_1fr] gap-2">
						<span class="text-text-faint">Region</span>
						<span class="font-mono text-text break-all">{dashboard.data?.config.region ?? '--'}</span>
					</div>
					<div class="grid grid-cols-[6.5rem_1fr] gap-2">
						<span class="text-text-faint">Account</span>
						<span class="font-mono text-text break-all">{dashboard.data?.config.accountId ?? '--'}</span>
					</div>
					<div class="grid grid-cols-[6.5rem_1fr] gap-2">
						<span class="text-text-faint">API URL</span>
						<span class="font-mono text-text break-all">{dashboard.data?.config.endpoint ?? '--'}</span>
					</div>
				</div>
				<p class="mt-2 text-[11px] text-text-faint">These values are currently read-only.</p>
			</div>
		</div>

		<div class="flex items-center justify-end gap-2 border-t border-border px-4 py-3">
			<button
				type="button"
				onclick={closeSettings}
				class="rounded-md border border-border px-3 py-1.5 text-xs text-text-muted hover:bg-bg-surface"
			>
				Cancel
			</button>
			<button
				type="button"
				onclick={applySettings}
				class="rounded-md border border-accent-strong bg-accent-muted px-3 py-1.5 text-xs text-accent hover:bg-accent/20"
			>
				Save
			</button>
		</div>
	</div>
{/if}
