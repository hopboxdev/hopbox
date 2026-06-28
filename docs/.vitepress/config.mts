import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'Hopbox',
  description: 'Self-hosted, vendor-neutral control plane for development environments.',
  cleanUrls: true,
  appearance: 'dark',
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: '/logo.svg' }],
    ['meta', { name: 'theme-color', content: '#08070d' }],
  ],
  themeConfig: {
    logo: '/logo.svg',
    nav: [
      { text: 'Guide', link: '/guide/what-is-hopbox' },
      { text: 'boxd', link: '/guide/boxd' },
      { text: 'Reference', link: '/reference/cli' },
      { text: 'v0.1', items: [{ text: 'Releases', link: 'https://github.com/hopboxdev/hopbox/releases' }] },
    ],
    sidebar: [
      {
        text: 'Introduction',
        items: [
          { text: 'What is Hopbox', link: '/guide/what-is-hopbox' },
          { text: 'Quickstart', link: '/guide/quickstart' },
        ],
      },
      {
        text: 'boxd — compute over SSH',
        items: [
          { text: 'Overview', link: '/guide/boxd' },
        ],
      },
      {
        text: 'Access',
        items: [
          { text: 'SSH & the front door', link: '/guide/ssh' },
          { text: 'Auth & multi-user', link: '/guide/auth' },
        ],
      },
      {
        text: 'Operate',
        items: [
          { text: 'Deploy a server', link: '/guide/deploy' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'CLI', link: '/reference/cli' },
          { text: 'hopboxd config', link: '/reference/hopboxd' },
          { text: 'boxd config', link: '/reference/boxd' },
        ],
      },
    ],
    search: { provider: 'local' },
    socialLinks: [{ icon: 'github', link: 'https://github.com/hopboxdev/hopbox' }],
    editLink: {
      pattern: 'https://github.com/hopboxdev/hopbox/edit/main/docs/:path',
      text: 'Edit this page on GitHub',
    },
    footer: {
      message: 'Self-hosted · open source',
      copyright: '© Hopbox',
    },
  },
})
