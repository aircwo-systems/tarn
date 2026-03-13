<script lang="ts">
  import { Package, HardDrive, ArrowClockwise, Eye, Code, CopySimple, Check } from 'phosphor-svelte';
  import LedDot from '$lib/components/common/led-dot.svelte';
  import { getDashboard } from '$lib/state.svelte';
  import { formatBytes } from '$lib/utils';

  const dashboard = getDashboard();

  const buckets = $derived(dashboard.data?.buckets ?? []);

  let selectedBucket = $state('');
  let objects = $state<any[]>([]);
  let loadingObjects = $state(false);
  let selectedObjectKey = $state('');
  let objectPreview = $state('');
  let objectContentType = $state('');
  let loadingPreview = $state(false);
  let previewError = $state('');
  
  // New Preview States
  let showRaw = $state(false);
  let copied = $state(false);

  const isImage = $derived(
    objectContentType.startsWith('image/') || 
    objectContentType === 'image/svg+xml'
  );

  async function browseBucket(name: string) {
    selectedBucket = name;
    loadingObjects = true;
    clearPreview();
    try {
      const resp = await fetch(`/_s3/${name}?list-type=2`);
      const text = await resp.text();
      const parser = new DOMParser();
      const xml = parser.parseFromString(text, 'text/xml');
      const contents = xml.querySelectorAll('Contents');
      objects = Array.from(contents).map((c) => ({
        key: c.querySelector('Key')?.textContent ?? '',
        size: parseInt(c.querySelector('Size')?.textContent ?? '0', 10),
        lastModified: c.querySelector('LastModified')?.textContent ?? '',
        etag: c.querySelector('ETag')?.textContent ?? ''
      }));
    } catch {
      objects = [];
    } finally {
      loadingObjects = false;
    }
  }

  function closeBrowser() {
    selectedBucket = '';
    objects = [];
    clearPreview();
  }

  function clearPreview() {
    selectedObjectKey = '';
    objectPreview = '';
    objectContentType = '';
    loadingPreview = false;
    previewError = '';
    showRaw = false;
    copied = false;
  }

  function encodeS3KeyPath(key: string): string {
    return key
      .split('/')
      .map((part) => encodeURIComponent(part))
      .join('/');
  }

  async function copyToClipboard() {
    try {
      await navigator.clipboard.writeText(objectPreview);
      copied = true;
      setTimeout(() => (copied = false), 2000);
    } catch (err) {
      console.error('Failed to copy!', err);
    }
  }

  async function viewObject(bucket: string, key: string) {
    selectedObjectKey = key;
    loadingPreview = true;
    objectPreview = '';
    objectContentType = '';
    previewError = '';
    showRaw = false;
    try {
      const path = encodeS3KeyPath(key);
      const resp = await fetch(`/_s3/${bucket}/${path}`);
      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}`);
      }
      objectContentType = resp.headers.get('content-type') ?? 'application/octet-stream';
      const text = await resp.text();
      objectPreview = text;
    } catch (error) {
      previewError = error instanceof Error ? error.message : 'Failed to load object';
    } finally {
      loadingPreview = false;
    }
  }
</script>

<div class="space-y-4">
  <div class="flex items-center justify-between gap-4 flex-wrap rounded-lg border border-border bg-card px-4 py-3">
    <div class="flex items-center gap-3">
      <div class="flex items-center justify-center h-8 w-8 rounded-md bg-accent/10">
        <HardDrive size={16} class="text-primary" />
      </div>
      <div>
        <h2 class="text-sm font-semibold text-foreground">S3 Storage</h2>
        <p class="text-[10px] text-muted-foreground/70 font-mono">
          {buckets.length} bucket{buckets.length !== 1 ? 's' : ''}
        </p>
      </div>
    </div>
    <span class="inline-flex items-center rounded-full border border-border px-2.5 py-1 text-xs text-muted-foreground/70">
      Path-style: /_s3/&lbrace;bucket&rbrace;/&lbrace;key&rbrace;
    </span>
  </div>

  {#if buckets.length === 0 && !dashboard.loading}
    <div class="rounded-lg border border-border bg-card px-4 py-8 text-center">
      <Package size={28} class="mx-auto text-muted-foreground/70 mb-2" />
      <p class="text-sm text-muted-foreground">No S3 buckets</p>
      <p class="text-xs text-muted-foreground/70 mt-1">
        Create one with <code class="bg-muted px-1 py-0.5 rounded text-primary">openstack s3 mb --name my-bucket</code>
      </p>
    </div>
  {:else}
    <div class="rounded-lg border border-border bg-card overflow-hidden">
      <table class="w-full text-xs">
        <thead>
          <tr class="border-b border-border bg-muted/50">
            <th class="text-left px-3 py-2 font-mono text-muted-foreground/70 uppercase tracking-wider">Bucket</th>
            <th class="text-right px-3 py-2 font-mono text-muted-foreground/70 uppercase tracking-wider">Objects</th>
            <th class="text-right px-3 py-2 font-mono text-muted-foreground/70 uppercase tracking-wider">Total Size</th>
            <th class="text-right px-3 py-2 font-mono text-muted-foreground/70 uppercase tracking-wider">Created</th>
            <th class="text-right px-3 py-2 font-mono text-muted-foreground/70 uppercase tracking-wider"></th>
          </tr>
        </thead>
        <tbody>
          {#each buckets as bucket}
            <tr class="border-b border-border last:border-b-0 hover:bg-muted/40 transition-colors">
              <td class="px-3 py-2">
                <div class="flex items-center gap-2">
                  <LedDot color="green" />
                  <span class="text-foreground font-mono">{bucket.name}</span>
                </div>
              </td>
              <td class="text-right px-3 py-2 text-muted-foreground font-mono">{bucket.objects}</td>
              <td class="text-right px-3 py-2 text-muted-foreground font-mono">{formatBytes(bucket.totalSize)}</td>
              <td class="text-right px-3 py-2 text-muted-foreground/70 font-mono">
                {new Date(bucket.createdDate).toLocaleDateString()}
              </td>
              <td class="text-right px-3 py-2">
                <button
                  type="button"
                  class="text-primary hover:underline text-[11px]"
                  onclick={() => browseBucket(bucket.name)}
                >
                  Browse
                </button>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}

  {#if selectedBucket}
    <div class="rounded-lg border border-primary/50 bg-card overflow-hidden">
      <div class="flex items-center justify-between px-3 py-2 border-b border-border bg-muted/30">
        <div class="flex items-center gap-2">
          <HardDrive size={13} class="text-primary" />
          <span class="text-xs font-mono text-foreground">s3://{selectedBucket}</span>
          <span class="text-[10px] text-muted-foreground/70 font-mono">({objects.length} objects)</span>
        </div>
        <div class="flex items-center gap-2">
          <button
            type="button"
            onclick={() => browseBucket(selectedBucket)}
            class="text-muted-foreground hover:text-foreground transition-colors"
            aria-label="Refresh objects"
          >
            <ArrowClockwise size={12} class={loadingObjects ? 'animate-spin' : ''} />
          </button>
          <button
            type="button"
            onclick={closeBrowser}
            class="text-xs text-muted-foreground hover:text-foreground transition-colors"
          >
            Close
          </button>
        </div>
      </div>

      {#if loadingObjects}
        <div class="px-3 py-6 text-center text-xs text-muted-foreground/70 font-mono">Loading objects...</div>
      {:else if objects.length === 0}
        <div class="px-3 py-6 text-center text-xs text-muted-foreground/70 font-mono">Bucket is empty</div>
      {:else}
        <div class="max-h-80 overflow-y-auto">
          <table class="w-full text-xs">
            <thead class="sticky top-0 bg-card">
              <tr class="border-b border-border">
                <th class="text-left px-3 py-1.5 font-mono text-muted-foreground/70 uppercase tracking-wider">Key</th>
                <th class="text-right px-3 py-1.5 font-mono text-muted-foreground/70 uppercase tracking-wider">Size</th>
                <th class="text-right px-3 py-1.5 font-mono text-muted-foreground/70 uppercase tracking-wider">Last Modified</th>
                <th class="text-right px-3 py-1.5 font-mono text-muted-foreground/70 uppercase tracking-wider"></th>
              </tr>
            </thead>
            <tbody>
              {#each objects as obj}
                <tr class="border-b border-border last:border-b-0">
                  <td class="px-3 py-1.5 text-foreground font-mono truncate max-w-[300px]" title={obj.key}>{obj.key}</td>
                  <td class="text-right px-3 py-1.5 text-muted-foreground font-mono whitespace-nowrap">{formatBytes(obj.size)}</td>
                  <td class="text-right px-3 py-1.5 text-muted-foreground/70 font-mono whitespace-nowrap">
                    {new Date(obj.lastModified).toLocaleString()}
                  </td>
                  <td class="text-right px-3 py-1.5">
                    <button
                      type="button"
                      class="text-primary hover:underline text-[11px]"
                      onclick={() => void viewObject(selectedBucket, obj.key)}
                    >
                      View
                    </button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>

        {#if selectedObjectKey}
          <div class="border-t border-border bg-muted/30">
            <div class="flex items-center justify-between px-3 py-2 border-b border-border">
              <div class="space-y-0.5 min-w-0">
                <p class="text-xs text-foreground font-mono truncate max-w-[20rem] sm:max-w-[32rem]" title={selectedObjectKey}>
                  {selectedObjectKey}
                </p>
                <p class="text-[10px] text-muted-foreground/70 font-mono">{objectContentType || '--'}</p>
              </div>
              
              <div class="flex items-center gap-3">
                {#if isImage}
                  <button
                    type="button"
                    class="flex items-center gap-1.5 text-[11px] {showRaw ? 'text-primary' : 'text-muted-foreground hover:text-foreground'}"
                    onclick={() => showRaw = !showRaw}
                  >
                    {#if showRaw}
                      <Eye size={14} />
                      <span>Preview</span>
                    {:else}
                      <Code size={14} />
                      <span>Raw</span>
                    {/if}
                  </button>
                {/if}

                {#if !isImage || showRaw}
                  <button
                    type="button"
                    class="flex items-center gap-1.5 text-[11px] transition-colors {copied ? 'text-green-400' : 'text-muted-foreground hover:text-foreground'}"
                    onclick={copyToClipboard}
                  >
                    {#if copied}
                      <Check size={14} />
                      <span>Copied</span>
                    {:else}
                      <CopySimple size={14} />
                      <span>Copy</span>
                    {/if}
                  </button>
                {/if}

                <button
                  type="button"
                  class="text-[11px] text-muted-foreground hover:text-foreground"
                  onclick={clearPreview}
                >
                  Close
                </button>
              </div>
            </div>

            {#if loadingPreview}
              <div class="px-3 py-6 text-center text-xs text-muted-foreground/70 font-mono">Loading object...</div>
            {:else if previewError}
              <div class="px-3 py-6 text-center text-xs text-destructive-300 font-mono">{previewError}</div>
            {:else}
              <div class="max-h-screen overflow-auto px-3 py-3">
                {#if isImage && !showRaw}
                  <div class="flex justify-center bg-black/5 rounded-md p-4 border border-border/50">
                    <img 
                      src={`/_s3/${selectedBucket}/${encodeS3KeyPath(selectedObjectKey)}`} 
                      alt={selectedObjectKey}
                      class="max-w-full h-auto shadow-sm" 
                    />
                  </div>
                {:else}
                  <pre class="text-xs font-mono text-foreground whitespace-pre-wrap break-words leading-relaxed">{objectPreview}</pre>
                {/if}
              </div>
            {/if}
          </div>
        {/if}
      {/if}
    </div>
  {/if}
</div>