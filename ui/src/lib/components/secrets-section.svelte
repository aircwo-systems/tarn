<script lang="ts">
	import { EyeIcon, EyeSlashIcon, KeyIcon } from 'phosphor-svelte';
	import { TableRow, TableCell } from '$lib/components/ui/table';
	import ResourceTable from './resource-table.svelte';
	import ArnCell from './arn-cell.svelte';
	import { fetchSecretValue } from '$lib/api';
	import { getDashboard, getDashboardFilters, matchesTagFilter } from '$lib/state.svelte';
	import { formatDate } from '$lib/utils';

	const dashboard = getDashboard();
	const filters = getDashboardFilters();
	const secrets = $derived(
		(dashboard.data?.secrets ?? []).filter((secret) => matchesTagFilter(secret.tags, filters.tagFilter))
	);

	let secretValues = $state<Record<string, string>>({});
	let secretValueTypes = $state<Record<string, string>>({});
	let secretVisible = $state<Record<string, boolean>>({});
	let secretLoading = $state<Record<string, boolean>>({});
	let secretErrors = $state<Record<string, string>>({});

	function hasLoadedSecretValue(name: string): boolean {
		return Object.prototype.hasOwnProperty.call(secretValues, name);
	}

	async function toggleSecretValue(name: string) {
		if (secretVisible[name]) {
			secretVisible = { ...secretVisible, [name]: false };
			return;
		}

		if (!hasLoadedSecretValue(name) && !secretLoading[name]) {
			await loadSecretValue(name);
		}

		if (!secretErrors[name]) {
			secretVisible = { ...secretVisible, [name]: true };
		}
	}

	async function loadSecretValue(name: string) {
		secretLoading = { ...secretLoading, [name]: true };
		secretErrors = { ...secretErrors, [name]: '' };

		try {
			const secret = await fetchSecretValue(name);
			secretValues = { ...secretValues, [name]: secret.value };
			secretValueTypes = { ...secretValueTypes, [name]: secret.valueType };
		} catch (error) {
			secretErrors = {
				...secretErrors,
				[name]: error instanceof Error ? error.message : 'Failed to load secret value'
			};
		} finally {
			secretLoading = { ...secretLoading, [name]: false };
		}
	}

	function renderSecretValue(name: string): string {
		if (secretLoading[name]) return 'Loading...';
		if (secretErrors[name]) return 'Load failed';
		if (!secretVisible[name]) return '********';

		const value = secretValues[name] ?? '';
		const valueType = secretValueTypes[name] ?? 'string';
		if (valueType === 'binary') {
			return value ? `${value} (base64)` : '(empty binary)';
		}
		return value || '(empty)';
	}
</script>

<ResourceTable
	title="Secrets Manager"
	count={secrets.length}
	loading={dashboard.loading && !dashboard.data}
	empty={secrets.length === 0}
	emptyMessage="No secrets created yet."
	emptyIcon={KeyIcon}
	columns={['Name', 'Description', 'Value', 'Version', 'Tags', 'Created', 'Changed']}
>
	{#each secrets as secret}
		<TableRow>
			<TableCell><ArnCell name={secret.name} arn={secret.arn} /></TableCell>
			<TableCell class="text-text-muted text-xs">{secret.description || '--'}</TableCell>
			<TableCell class="min-w-[18rem]">
				<div class="flex items-center gap-2">
					<span
						class={`max-w-[20rem] break-all font-mono text-xs ${secretErrors[secret.name] ? 'text-red-300' : 'text-text-faint'}`}
						title={secretVisible[secret.name] ? renderSecretValue(secret.name) : 'Hidden'}
					>
						{renderSecretValue(secret.name)}
					</span>
					<button
						type="button"
						class="shrink-0 rounded-md border border-border p-1 text-text-muted hover:bg-bg-subtle hover:text-text disabled:cursor-not-allowed disabled:opacity-50"
						onclick={() => void toggleSecretValue(secret.name)}
						disabled={secretLoading[secret.name]}
						title={secretVisible[secret.name] ? 'Hide secret value' : 'View secret value'}
						aria-label={secretVisible[secret.name]
							? `Hide secret value for ${secret.name}`
							: `View secret value for ${secret.name}`}
					>
						{#if secretVisible[secret.name]}
							<EyeSlashIcon size={14} />
						{:else}
							<EyeIcon size={14} />
						{/if}
					</button>
				</div>
			</TableCell>
			<TableCell class="font-mono text-text-faint text-xs">{secret.versionId}</TableCell>
			<TableCell class="text-text-muted">{secret.tagCount}</TableCell>
			<TableCell class="text-text-faint text-xs">{formatDate(secret.createdDate)}</TableCell>
			<TableCell class="text-text-faint text-xs">{formatDate(secret.lastChangedDate)}</TableCell>
		</TableRow>
	{/each}
</ResourceTable>
