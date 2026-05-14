<script lang="ts">
  import * as Dialog from "$lib/components/ui/dialog";
  import { XIcon, CopyIcon, CheckIcon, LightningIcon } from "phosphor-svelte";
  import { formatBytes, formatDate } from "$lib/utils";
  import type { FunctionSummary } from "$lib/types";
  import LedDot from "$lib/components/common/led-dot.svelte";

  let {
    open = $bindable(false),
    fn,
  }: {
    open?: boolean;
    fn: FunctionSummary | null;
  } = $props();

  let copied = $state(false);

  function stateColor(state: string): "green" | "amber" | "red" | "gray" {
    const normalized = state.toLowerCase();
    if (normalized === "active") return "green";
    if (normalized === "pending") return "amber";
    if (normalized === "failed" || normalized === "inactive") return "red";
    return "gray";
  }

  function buildHcl(f: FunctionSummary): string {
    const COL = 13;
    const s = (key: string, val: string) => `  ${key.padEnd(COL)} = ${val}`;
    const q = (v: string) => `"${v}"`;

    const lines: string[] = [];
    lines.push(`resource "aws_lambda_function" ${q(f.name)} {`);
    lines.push(s("function_name", q(f.name)));
    lines.push(s("runtime",       q(f.runtime)));
    lines.push(s("memory_size",   String(f.memoryMB)));
    lines.push(s("timeout",       String(f.timeoutSec)));
    lines.push(``);
    lines.push(`  # deployment`);
    lines.push(s("code_size",     q(formatBytes(f.codeSize))));
    if (f.layers > 0) lines.push(s("layers", String(f.layers)));
    lines.push(s("version",       q(f.version)));
    lines.push(``);
    lines.push(`  # metadata`);
    lines.push(s("state",         q(f.state)));
    lines.push(s("last_modified", q(formatDate(f.lastModified))));
    lines.push(s("arn",           q(f.arn)));
    if (f.tags && Object.keys(f.tags).length > 0) {
      lines.push(``);
      lines.push(`  tags = {`);
      for (const [k, v] of Object.entries(f.tags)) {
        lines.push(`    ${k} = ${q(v)}`);
      }
      lines.push(`  }`);
    }
    lines.push(`}`);
    return lines.join("\n");
  }

  const hcl = $derived(fn ? buildHcl(fn) : "");

  async function copyHcl() {
    await navigator.clipboard.writeText(hcl);
    copied = true;
    setTimeout(() => (copied = false), 1800);
  }

  // Tokenise a single HCL line into spans
  interface Token { type: "keyword" | "key" | "string" | "number" | "comment" | "brace" | "plain"; text: string }

  function tokenizeLine(line: string): Token[] {
    const tokens: Token[] = [];

    // comment
    if (/^\s*#/.test(line)) {
      tokens.push({ type: "comment", text: line });
      return tokens;
    }

    // resource keyword line
    const resourceMatch = line.match(/^(\s*)(resource)\s+("[\w-]+")\s+("[\w-]+")\s+(\{)$/);
    if (resourceMatch) {
      tokens.push({ type: "plain",   text: resourceMatch[1] });
      tokens.push({ type: "keyword", text: resourceMatch[2] });
      tokens.push({ type: "plain",   text: " " });
      tokens.push({ type: "string",  text: resourceMatch[3] });
      tokens.push({ type: "plain",   text: " " });
      tokens.push({ type: "string",  text: resourceMatch[4] });
      tokens.push({ type: "plain",   text: " " });
      tokens.push({ type: "brace",   text: resourceMatch[5] });
      return tokens;
    }

    // closing brace lines (  } or })
    const braceOnly = line.match(/^(\s*)(\})$/);
    if (braceOnly) {
      tokens.push({ type: "plain", text: braceOnly[1] });
      tokens.push({ type: "brace", text: braceOnly[2] });
      return tokens;
    }

    // tags = { line
    const tagsOpen = line.match(/^(\s*)(tags)\s*(=)\s*(\{)$/);
    if (tagsOpen) {
      tokens.push({ type: "plain",   text: tagsOpen[1] });
      tokens.push({ type: "key",     text: tagsOpen[2] });
      tokens.push({ type: "plain",   text: " = " });
      tokens.push({ type: "brace",   text: tagsOpen[4] });
      return tokens;
    }

    // key = "string value"
    const kvString = line.match(/^(\s*)([\w_-]+)(\s*=\s*)(".*")$/);
    if (kvString) {
      tokens.push({ type: "plain",  text: kvString[1] });
      tokens.push({ type: "key",    text: kvString[2] });
      tokens.push({ type: "plain",  text: kvString[3] });
      tokens.push({ type: "string", text: kvString[4] });
      return tokens;
    }

    // key = number
    const kvNumber = line.match(/^(\s*)([\w_-]+)(\s*=\s*)(\d+)(.*)$/);
    if (kvNumber) {
      tokens.push({ type: "plain",  text: kvNumber[1] });
      tokens.push({ type: "key",    text: kvNumber[2] });
      tokens.push({ type: "plain",  text: kvNumber[3] });
      tokens.push({ type: "number", text: kvNumber[4] });
      if (kvNumber[5]) tokens.push({ type: "plain", text: kvNumber[5] });
      return tokens;
    }

    tokens.push({ type: "plain", text: line });
    return tokens;
  }

  const lines = $derived(hcl.split("\n").map((l, i) => ({ num: i + 1, tokens: tokenizeLine(l) })));

  function tokenStyle(type: Token["type"]): string {
    switch (type) {
      case "keyword": return "color:var(--lh-key)";
      case "key":     return "color:var(--lh-key)";
      case "string":  return "color:var(--lh-str-json)";
      case "number":  return "color:var(--lh-num)";
      case "comment": return "color:var(--lh-null);font-style:italic";
      case "brace":   return "color:var(--lh-punct)";
      default:        return "";
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Content
    showCloseButton={false}
    class="flex max-h-[88vh] w-full max-w-4xl sm:max-w-4xl lg:max-w-5xl xl:max-w-6xl flex-col gap-0 overflow-hidden border-border/80 p-0"
  >
    <!-- Header -->
    <div class="flex items-center justify-between border-b border-border/60 px-6 py-4">
      <div class="flex items-center gap-3.5">
        <div class="flex h-9 w-9 items-center justify-center rounded-md border border-border bg-muted/40 text-primary">
          <LightningIcon size={16} weight="fill" />
        </div>
        <div>
          <Dialog.Title class="font-mono text-[15px] font-semibold tracking-tight text-foreground">
            {fn?.name ?? ""}
          </Dialog.Title>
          <p class="mt-0.5 font-mono text-[11px] text-muted-foreground/55">{fn?.runtime ?? ""}</p>
        </div>
      </div>
      <div class="flex items-center gap-3">
        {#if fn}
          <span class="inline-flex items-center gap-1.5 rounded-full border border-border/60 px-2.5 py-1 text-[11px] text-muted-foreground">
            <LedDot color={stateColor(fn.state)} />
            {fn.state}
          </span>
        {/if}
        <Dialog.Close
          class="flex h-7 w-7 items-center justify-center rounded-md border border-border/60 text-muted-foreground transition-colors hover:border-border hover:bg-muted/40 hover:text-foreground"
        >
          <XIcon size={13} />
        </Dialog.Close>
      </div>
    </div>

    <!-- Body: two-column layout -->
    <div class="flex min-h-0 flex-1 overflow-hidden">

      <!-- Left: metadata panel -->
      {#if fn}
        <div class="flex w-[240px] shrink-0 flex-col overflow-y-auto border-r border-border/60 p-5">
          <div class="flex flex-col gap-5">
            {#each [
              { label: "Memory",    value: `${fn.memoryMB} MB`          },
              { label: "Timeout",   value: `${fn.timeoutSec}s`          },
              { label: "Code size", value: formatBytes(fn.codeSize)     },
              { label: "Layers",    value: String(fn.layers)            },
              { label: "Version",   value: fn.version                   },
              { label: "Messages",  value: fn.messagesProcessed.toLocaleString("en-GB") },
              { label: "Modified",  value: formatDate(fn.lastModified)  },
            ] as stat}
              <div class="flex flex-col gap-1">
                <span class="text-[10px] uppercase tracking-widest text-muted-foreground/45">{stat.label}</span>
                <span class="font-mono text-[13px] font-semibold tabular-nums text-foreground">{stat.value}</span>
              </div>
            {/each}

            {#if fn.tags && Object.keys(fn.tags).length > 0}
              <div class="flex flex-col gap-3 border-t border-border/40 pt-5">
                <span class="text-[10px] uppercase tracking-widest text-muted-foreground/45">Tags</span>
                {#each Object.entries(fn.tags) as [k, v]}
                  <div class="flex flex-col gap-0.5">
                    <span class="text-[10px] text-muted-foreground/50">{k}</span>
                    <span class="break-all font-mono text-[11px] text-foreground/75">{v}</span>
                  </div>
                {/each}
              </div>
            {/if}
          </div>

          <!-- ARN pinned to bottom -->
          <div class="mt-auto flex flex-col gap-1.5 border-t border-border/40 pt-5">
            <span class="text-[10px] uppercase tracking-widest text-muted-foreground/45">ARN</span>
            <span class="break-all font-mono text-[10px] leading-relaxed text-muted-foreground/40" title={fn.arn}>{fn.arn}</span>
          </div>
        </div>
      {/if}

      <!-- Right: code block -->
      <div class="flex min-w-0 flex-1 flex-col overflow-hidden bg-muted/15">
        <!-- toolbar -->
        <div class="flex items-center justify-between border-b border-border/40 bg-muted/25 px-5 py-2 backdrop-blur">
          <span class="font-mono text-[11px] text-muted-foreground/45">terraform · aws_lambda_function</span>
          <button
            onclick={copyHcl}
            class="flex items-center gap-1.5 rounded px-2.5 py-1 font-mono text-[11px] text-muted-foreground/55 transition-colors hover:bg-muted/60 hover:text-foreground"
          >
            {#if copied}
              <CheckIcon size={11} class="text-[#00E5A0]" />
              <span class="text-[#00E5A0]">copied</span>
            {:else}
              <CopyIcon size={11} />
              <span>copy</span>
            {/if}
          </button>
        </div>

        <div class="flex-1 overflow-auto px-5 py-4">
          <pre class="font-mono text-[12.5px] leading-[1.75]"><code>{#each lines as line (line.num)}<span class="flex"><span class="mr-5 w-6 select-none text-right font-mono text-muted-foreground/25">{line.num}</span><span class="flex-1">{#each line.tokens as tok}<span style={tokenStyle(tok.type)}>{tok.text}</span>{/each}</span></span>{/each}</code></pre>
        </div>
      </div>

    </div>
  </Dialog.Content>
</Dialog.Root>
