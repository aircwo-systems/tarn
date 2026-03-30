<script setup lang="ts">
import { computed } from 'vue'
import { useData } from 'vitepress'

const { theme } = useData()

const version = computed(() => {
  const docsVersion = (theme.value as { docsVersion?: string }).docsVersion
  return docsVersion?.trim() || 'dev'
})
</script>

<template>
  <code>{{ version }}</code>
</template>
