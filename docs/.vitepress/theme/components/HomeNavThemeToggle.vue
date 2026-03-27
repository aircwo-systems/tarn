<script setup lang="ts">
import { computed } from 'vue'
import { useData, useRoute } from 'vitepress'
import VPSwitchAppearance from 'vitepress/dist/client/theme-default/components/VPSwitchAppearance.vue'

const route = useRoute()
const { frontmatter, site, theme } = useData()

const docsVersion = computed(() => {
  return (theme.value as typeof theme.value & { docsVersion?: string }).docsVersion
})

const showToggle = computed(() => {
  const appearanceEnabled =
    site.value.appearance &&
    site.value.appearance !== 'force-dark' &&
    site.value.appearance !== 'force-auto'

  return appearanceEnabled && route.path === '/' && frontmatter.value.layout === 'home'
})
</script>

<template>
  <div v-if="docsVersion || showToggle" class="HomeNavThemeToggle">
    <span v-if="docsVersion" class="DocsVersionBadge">{{ docsVersion }}</span>
    <VPSwitchAppearance v-if="showToggle" />
  </div>
</template>
