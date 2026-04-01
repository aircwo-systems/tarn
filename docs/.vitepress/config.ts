import { execFileSync, execSync } from 'node:child_process'
import { defineConfig } from 'vitepress'

function getDocsVersion() {
  if (process.env.DOCS_VERSION?.trim()) {
    return process.env.DOCS_VERSION.trim()
  }

  try {
    return execSync('git describe --tags --abbrev=0', {
      encoding: 'utf8'
    }).trim()
  } catch {
    try {
      return execSync('git describe --tags --always', {
        encoding: 'utf8'
      }).trim()
    } catch {
      return 'dev'
    }
  }
}

function getPageLastUpdated(filePath: string) {
  try {
    const value = execFileSync('git', ['log', '-1', '--format=%ct', '--', filePath], {
      encoding: 'utf8'
    }).trim()

    if (!value) {
      return undefined
    }

    const seconds = Number.parseInt(value, 10)
    return Number.isNaN(seconds) ? undefined : seconds * 1000
  } catch {
    return undefined
  }
}

export default defineConfig({
  title: 'Tarn',
  description: 'Local AWS cloud emulator for development and testing',
  lang: 'en-US',
  lastUpdated: true,
  base: '/tarn/',

  head: [
    ['link', { rel: 'icon', href: '/favicon.svg', type: 'image/svg+xml' }],
    ['link', { rel: 'shortcut icon', href: '/favicon.svg', type: 'image/svg+xml' }],
    ['meta', { name: 'theme-color', media: '(prefers-color-scheme: dark)', content: '#1b1b1f' }],
    ['meta', { name: 'theme-color', media: '(prefers-color-scheme: light)', content: '#ffffff' }],
  ],

  themeConfig: {
    logo: '/favicon.svg',

    nav: [
      { text: 'Home', link: '/' },
      { text: 'Getting Started', link: '/guide/getting-started' },
      { text: 'Services', link: '/services/' },
      { text: 'Terraform', link: '/guide/terraform' },
      { text: 'GitHub', link: 'https://github.com/aircwo-systems/tarn' }
    ],

    sidebar: {
      '/guide/': [
        {
          text: 'Getting Started',
          items: [
            { text: 'Quick Start', link: '/guide/getting-started' },
            { text: 'Installation', link: '/guide/installation' },
            { text: 'Configuration', link: '/guide/configuration' },
            { text: 'Terraform', link: '/guide/terraform' },
          ]
        },
        {
          text: 'Development',
          items: [
            { text: 'Contributing', link: '/guide/contributing' },
            { text: 'Building from Source', link: '/guide/development' },
          ]
        }
      ],
      '/services/': [
        {
          text: 'Services',
          items: [
            { text: 'Overview', link: '/services/' },
            { text: 'Lambda', link: '/services/lambda' },
            { text: 'API Gateway', link: '/services/api-gateway' },
            { text: 'S3', link: '/services/s3' },
            { text: 'SQS', link: '/services/sqs' },
            { text: 'SNS', link: '/services/sns' },
            { text: 'Secrets Manager', link: '/services/secrets-manager' },
            { text: 'EventBridge', link: '/services/eventbridge' },
            { text: 'IAM', link: '/services/iam' },
          ]
        }
      ],
      '/reference/': [
        {
          text: 'Reference',
          items: [
            { text: 'API Coverage', link: '/reference/api-coverage' },
          ]
        }
      ]
    },

    socialLinks: [
      { icon: 'github', link: 'https://github.com/aircwo-systems/tarn' }
    ],

    docsVersion: getDocsVersion(),

    footer: {
      message: 'Released under the Apache 2.0 License',
      copyright: 'Copyright © 2026 Aircwo Systems'
    },

    lastUpdated: {
      text: 'Last updated'
    }
  },

  markdown: {
    theme: {
      dark: 'github-dark',
      light: 'github-light'
    }
  },

  transformPageData(pageData) {
    const lastUpdated = getPageLastUpdated(pageData.filePath)

    if (!lastUpdated) {
      return
    }

    return {
      lastUpdated
    }
  }
})
