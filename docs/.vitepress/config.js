import { lysConfig } from 'lys-docs-theme/config'

export default lysConfig({
  name: 'lys-settings',
  description: "The lys desktop's settings app, for lys-shell and lys-wm.",
  nav: [{ text: 'Guide', link: '/guide/getting-started' }],
  sidebar: [
    {
      text: 'Guide',
      items: [
        { text: 'Getting started', link: '/guide/getting-started' },
        { text: 'Install', link: '/guide/install' },
        { text: 'lys-wm settings', link: '/guide/lys-wm' },
        { text: 'How it works', link: '/guide/how-it-works' },
        { text: 'Development', link: '/guide/development' },
      ],
    },
  ],
})
