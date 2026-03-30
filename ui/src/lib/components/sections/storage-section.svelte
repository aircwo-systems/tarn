<script lang="ts">
  import {
    Package,
    HardDrive,
    ArrowClockwise,
    Eye,
    Code,
    CopySimple,
    Check,
  } from "phosphor-svelte";
  import LedDot from "$lib/components/common/led-dot.svelte";
  import { getDashboard } from "$lib/state.svelte";
  import { formatBytes } from "$lib/utils";
  import {
    Accordion,
    AccordionItem,
    AccordionTrigger,
    AccordionContent,
  } from "$lib/components/ui/accordion";
  import SectionHeader from "./section-header.svelte";

  type BucketObject = {
    key: string;
    size: number;
    lastModified: string;
    etag: string;
  };

  type ObjectPreviewState = {
    loading: boolean;
    error: string;
    content: string;
    contentType: string;
    showRaw: boolean;
    copied: boolean;
  };

  let {
    sidebarCollapsed = false,
    onToggleSidebar = () => {},
  }: {
    sidebarCollapsed?: boolean;
    onToggleSidebar?: () => void;
  } = $props();

  const dashboard = getDashboard();
  const buckets = $derived(dashboard.data?.buckets ?? []);

  let selectedBucket = $state("");
  let objects = $state<BucketObject[]>([]);
  let loadingObjects = $state(false);
  let openObjectKeys = $state<string[]>([]);
  let previewByKey = $state<Record<string, ObjectPreviewState>>({});

  function defaultPreviewState(): ObjectPreviewState {
    return {
      loading: false,
      error: "",
      content: "",
      contentType: "",
      showRaw: false,
      copied: false,
    };
  }

  function ensurePreviewState(key: string): ObjectPreviewState {
    const existing = previewByKey[key];
    if (existing) return existing;

    const created = defaultPreviewState();
    previewByKey = { ...previewByKey, [key]: created };
    return created;
  }

  function patchPreviewState(key: string, patch: Partial<ObjectPreviewState>) {
    const current = ensurePreviewState(key);
    previewByKey = {
      ...previewByKey,
      [key]: {
        ...current,
        ...patch,
      },
    };
  }

  function clearAllPreviews() {
    openObjectKeys = [];
    previewByKey = {};
  }

  function closeBrowser() {
    selectedBucket = "";
    objects = [];
    clearAllPreviews();
  }

  function isObjectOpen(key: string): boolean {
    return openObjectKeys.includes(key);
  }

  function isImageContent(contentType: string): boolean {
    return contentType.startsWith("image/") || contentType === "image/svg+xml";
  }

  function encodeS3KeyPath(key: string): string {
    return key
      .split("/")
      .map((part) => encodeURIComponent(part))
      .join("/");
  }

  async function browseBucket(name: string) {
    selectedBucket = name;
    loadingObjects = true;
    clearAllPreviews();

    try {
      const resp = await fetch(`/_s3/${name}?list-type=2`);
      const text = await resp.text();
      const parser = new DOMParser();
      const xml = parser.parseFromString(text, "text/xml");
      const contents = xml.querySelectorAll("Contents");

      objects = Array.from(contents).map((c) => ({
        key: c.querySelector("Key")?.textContent ?? "",
        size: parseInt(c.querySelector("Size")?.textContent ?? "0", 10),
        lastModified: c.querySelector("LastModified")?.textContent ?? "",
        etag: c.querySelector("ETag")?.textContent ?? "",
      }));
    } catch {
      objects = [];
    } finally {
      loadingObjects = false;
    }
  }

  async function loadObjectPreview(bucket: string, key: string, force = false) {
    const state = ensurePreviewState(key);
    if (!force && (state.loading || state.content || state.error)) {
      return;
    }

    patchPreviewState(key, {
      loading: true,
      error: "",
      copied: false,
      showRaw: false,
    });

    try {
      const path = encodeS3KeyPath(key);
      const resp = await fetch(`/_s3/${bucket}/${path}`);
      if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}`);
      }

      const contentType = resp.headers.get("content-type") ?? "application/octet-stream";
      const text = await resp.text();

      patchPreviewState(key, {
        loading: false,
        error: "",
        content: text,
        contentType,
      });
    } catch (error) {
      patchPreviewState(key, {
        loading: false,
        contentType: "",
        content: "",
        error: error instanceof Error ? error.message : "Failed to load object",
      });
    }
  }

  function handleObjectOpenChange(next: string[] | string) {
    const nextValues = Array.isArray(next)
      ? next
      : next
          ? [next]
          : [];

    const newlyOpened = nextValues.filter((key) => !openObjectKeys.includes(key));
    openObjectKeys = nextValues;

    for (const key of newlyOpened) {
      void loadObjectPreview(selectedBucket, key);
    }
  }

  async function copyToClipboard(key: string) {
    const preview = ensurePreviewState(key);
    if (!preview.content) return;

    try {
      await navigator.clipboard.writeText(preview.content);
      patchPreviewState(key, { copied: true });
      setTimeout(() => {
        const latest = previewByKey[key];
        if (!latest) return;
        patchPreviewState(key, { copied: false });
      }, 2000);
    } catch (err) {
      console.error("Failed to copy object preview", err);
    }
  }

  function toggleRawPreview(key: string) {
    const preview = ensurePreviewState(key);
    patchPreviewState(key, { showRaw: !preview.showRaw });
  }
</script>

<div class="space-y-4">
  <SectionHeader
    title="S3 storage"
    description="Buckets, object previews and direct Tarn storage paths."
    icon={HardDrive}
    {sidebarCollapsed}
    {onToggleSidebar}
  >
    {#snippet stats()}
      <span class="inline-flex items-center gap-1.5">
        <span class="font-mono text-foreground">{buckets.length}</span>
        <span class="text-muted-foreground/70">
          bucket{buckets.length !== 1 ? "s" : ""}
        </span>
      </span>
    {/snippet}

    {#snippet actions()}
      <span class="text-[11px] text-muted-foreground/50 font-mono">
        /_s3/&lbrace;bucket&rbrace;/&lbrace;key&rbrace;
      </span>
    {/snippet}
  </SectionHeader>

  {#if buckets.length === 0 && !dashboard.loading}
    <div class="rounded-lg border border-border bg-card px-4 py-8 text-center">
      <Package size={28} class="mx-auto text-muted-foreground/70 mb-2" />
      <p class="text-sm text-muted-foreground">No S3 buckets</p>
      <p class="text-xs text-muted-foreground/70 mt-1">
        Create one with <code class="bg-muted px-1 py-0.5 rounded text-primary">tarn s3 mb --name my-bucket</code>
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
                  onclick={() => void browseBucket(bucket.name)}
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
            onclick={() => void browseBucket(selectedBucket)}
            class="text-muted-foreground hover:text-foreground transition-colors"
            aria-label="Refresh objects"
          >
            <ArrowClockwise size={12} class={loadingObjects ? "animate-spin" : ""} />
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
        <div class="max-h-96 overflow-y-auto px-3">
          <div class="grid grid-cols-[minmax(0,1fr)_auto_auto_auto] gap-3 border-b border-border py-1.5 text-[10px] text-muted-foreground/70 font-mono uppercase tracking-wider">
            <span>Key</span>
            <span class="text-right">Size</span>
            <span class="text-right">Last Modified</span>
            <span class="text-right">Action</span>
          </div>

          <Accordion
            type="multiple"
            value={openObjectKeys}
            onValueChange={handleObjectOpenChange}
            class="w-full"
          >
            {#each objects as obj}
              {@const preview = previewByKey[obj.key]}
              <AccordionItem value={obj.key}>
                <AccordionTrigger class="w-full py-1.5 text-foreground hover:bg-muted/20">
                  <div class="grid w-full grid-cols-[minmax(0,1fr)_auto_auto_auto] items-center gap-3 pr-2">
                    <span class="truncate font-mono" title={obj.key}>{obj.key}</span>
                    <span class="text-right text-muted-foreground font-mono whitespace-nowrap">{formatBytes(obj.size)}</span>
                    <span class="text-right text-muted-foreground/70 font-mono whitespace-nowrap">{new Date(obj.lastModified).toLocaleString()}</span>
                    <span class="text-right text-primary text-[11px]">{isObjectOpen(obj.key) ? "Close" : "View"}</span>
                  </div>
                </AccordionTrigger>

                <AccordionContent class="pt-0">
                  <div class="rounded-md border border-border bg-muted/20 px-3 py-2 mb-2">
                    <div class="flex items-center justify-between border-b border-border pb-2 mb-2">
                      <p class="text-[10px] text-muted-foreground/70 font-mono">{preview?.contentType || "--"}</p>

                      <div class="flex items-center gap-3">
                        {#if preview && isImageContent(preview.contentType)}
                          <button
                            type="button"
                            class="flex items-center gap-1.5 text-[11px] {preview.showRaw ? 'text-primary' : 'text-muted-foreground hover:text-foreground'}"
                            onclick={() => toggleRawPreview(obj.key)}
                          >
                            {#if preview.showRaw}
                              <Eye size={14} />
                              <span>Preview</span>
                            {:else}
                              <Code size={14} />
                              <span>Raw</span>
                            {/if}
                          </button>
                        {/if}

                        {#if preview && (!isImageContent(preview.contentType) || preview.showRaw)}
                          <button
                            type="button"
                            class="flex items-center gap-1.5 text-[11px] transition-colors {preview.copied ? 'text-green-400' : 'text-muted-foreground hover:text-foreground'}"
                            onclick={() => void copyToClipboard(obj.key)}
                          >
                            {#if preview.copied}
                              <Check size={14} />
                              <span>Copied</span>
                            {:else}
                              <CopySimple size={14} />
                              <span>Copy</span>
                            {/if}
                          </button>
                        {/if}
                      </div>
                    </div>

                    {#if !preview || preview.loading}
                      <div class="py-4 text-center text-xs text-muted-foreground/70 font-mono">Loading object...</div>
                    {:else if preview.error}
                      <div class="py-4 text-center text-xs text-destructive-300 font-mono">{preview.error}</div>
                    {:else if isImageContent(preview.contentType) && !preview.showRaw}
                      <div class="flex justify-center bg-black/5 rounded-md p-4 border border-border/50">
                        <img
                          src={`/_s3/${selectedBucket}/${encodeS3KeyPath(obj.key)}`}
                          alt={obj.key}
                          class="max-w-full h-auto shadow-sm"
                        />
                      </div>
                    {:else}
                      <pre class="text-xs font-mono text-foreground whitespace-pre-wrap break-words leading-relaxed">{preview.content}</pre>
                    {/if}
                  </div>
                </AccordionContent>
              </AccordionItem>
            {/each}
          </Accordion>
        </div>
      {/if}
    </div>
  {/if}
</div>
