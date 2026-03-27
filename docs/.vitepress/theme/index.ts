import { h } from 'vue'
import type { Theme } from 'vitepress'
import DefaultTheme from 'vitepress/theme'
import HomeNavThemeToggle from './components/HomeNavThemeToggle.vue'
import SidebarThemeToggle from './components/SidebarThemeToggle.vue'
import './custom.css'

export default {
  extends: DefaultTheme,
  Layout: () => {
    return h(DefaultTheme.Layout, null, {
      'nav-bar-content-after': () => h(HomeNavThemeToggle),
      'sidebar-nav-after': () => h(SidebarThemeToggle)
    })
  }
} satisfies Theme
