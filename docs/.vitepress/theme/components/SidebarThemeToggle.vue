<script setup lang="ts">
import { computed } from 'vue'
import { useData } from 'vitepress'
import VPSwitchAppearance from 'vitepress/dist/client/theme-default/components/VPSwitchAppearance.vue'

const { site } = useData()

const showAppearanceToggle = computed(() => {
  return (
    site.value.appearance &&
    site.value.appearance !== 'force-dark' &&
    site.value.appearance !== 'force-auto'
  )
})
</script>

<template>
  <div v-if="showAppearanceToggle" class="SidebarThemeToggle">
    <span class="label">Theme</span>
    <VPSwitchAppearance />
  </div>
</template>
