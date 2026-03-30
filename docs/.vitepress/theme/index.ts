import { h } from 'vue'
import type { Theme } from 'vitepress'
import DefaultTheme from 'vitepress/theme'
import DocsVersionCode from './components/DocsVersionCode.vue'
import HomeNavThemeToggle from './components/HomeNavThemeToggle.vue'
import ReleaseDownloadTabs from './components/ReleaseDownloadTabs.vue'
import SidebarThemeToggle from './components/SidebarThemeToggle.vue'
import './custom.css'

export default {
  extends: DefaultTheme,
  enhanceApp({ app }) {
    app.component('DocsVersionCode', DocsVersionCode)
    app.component('ReleaseDownloadTabs', ReleaseDownloadTabs)
  },
  Layout: () => {
    return h(DefaultTheme.Layout, null, {
      'nav-bar-content-after': () => h(HomeNavThemeToggle),
      'sidebar-nav-after': () => h(SidebarThemeToggle)
    })
  }
} satisfies Theme
