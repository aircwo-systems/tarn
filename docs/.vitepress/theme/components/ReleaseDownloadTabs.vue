<script setup lang="ts">
import { computed, ref } from 'vue'
import { useData } from 'vitepress'

type DownloadTab = {
  label: string
  shell: 'bash' | 'powershell'
  command: (version: string) => string
}

const tabs: DownloadTab[] = [
  {
    label: 'macOS Silicon',
    shell: 'bash',
    command: (version) =>
      `curl -L https://github.com/aircwo-systems/tarn/releases/download/${version}/tarn-darwin-arm64.tar.gz \\\n  | tar xz`
  },
  {
    label: 'macOS Intel',
    shell: 'bash',
    command: (version) =>
      `curl -L https://github.com/aircwo-systems/tarn/releases/download/${version}/tarn-darwin-amd64.tar.gz \\\n  | tar xz`
  },
  {
    label: 'Linux',
    shell: 'bash',
    command: (version) =>
      `curl -L https://github.com/aircwo-systems/tarn/releases/download/${version}/tarn-linux-amd64.tar.gz \\\n  | tar xz`
  },
  {
    label: 'Windows',
    shell: 'powershell',
    command: (version) =>
      `Invoke-WebRequest -Uri "https://github.com/aircwo-systems/tarn/releases/download/${version}/tarn-windows-amd64.zip" -OutFile tarn.zip\nExpand-Archive tarn.zip -DestinationPath .`
  }
]

const { theme } = useData()
const activeTab = ref(0)

const version = computed(() => {
  const docsVersion = (theme.value as { docsVersion?: string }).docsVersion
  return docsVersion?.trim() || 'dev'
})

const activeDownload = computed(() => tabs[activeTab.value] ?? tabs[0])
const activeCommand = computed(() => activeDownload.value.command(version.value))
</script>

<template>
  <div class="release-downloads">
    <div class="release-downloads__tabs" role="tablist" aria-label="Release download platforms">
      <button
        v-for="(tab, index) in tabs"
        :key="tab.label"
        :id="`release-download-tab-${index}`"
        class="release-downloads__tab"
        :class="{ 'release-downloads__tab--active': activeTab === index }"
        role="tab"
        type="button"
        :aria-selected="activeTab === index"
        :aria-controls="`release-download-panel-${index}`"
        @click="activeTab = index"
      >
        {{ tab.label }}
      </button>
    </div>

    <div
      :id="`release-download-panel-${activeTab}`"
      class="release-downloads__panel"
      role="tabpanel"
      :aria-labelledby="`release-download-tab-${activeTab}`"
    >
      <pre :class="['release-downloads__code', `language-${activeDownload.shell}`]"><code>{{ activeCommand }}</code></pre>
    </div>
  </div>
</template>

<style scoped>
.release-downloads {
  margin: 1rem 0 1.5rem;
  border: 1px solid var(--vp-c-divider);
  border-radius: 16px;
  background: color-mix(in srgb, var(--vp-c-bg-soft) 78%, transparent);
  overflow: hidden;
}

.release-downloads__tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 0.5rem;
  padding: 0.75rem;
  border-bottom: 1px solid var(--vp-c-divider);
  background: color-mix(in srgb, var(--vp-c-bg-alt) 65%, transparent);
}

.release-downloads__tab {
  appearance: none;
  border: 1px solid transparent;
  border-radius: 999px;
  background: transparent;
  color: var(--vp-c-text-2);
  cursor: pointer;
  font: inherit;
  font-size: 0.9rem;
  font-weight: 600;
  line-height: 1;
  padding: 0.6rem 0.9rem;
  transition: border-color 0.2s ease, background-color 0.2s ease, color 0.2s ease;
}

.release-downloads__tab:hover {
  border-color: var(--vp-c-divider);
  color: var(--vp-c-text-1);
}

.release-downloads__tab--active {
  border-color: color-mix(in srgb, var(--vp-c-brand-1) 35%, var(--vp-c-divider));
  background: color-mix(in srgb, var(--vp-c-brand-soft) 65%, transparent);
  color: var(--vp-c-text-1);
}

.release-downloads__panel {
  padding: 0.9rem;
}

.release-downloads__code {
  margin: 0;
  padding: 1rem 1.1rem;
  border-radius: 12px;
  background: var(--vp-code-block-bg);
  overflow-x: auto;
}
</style>
