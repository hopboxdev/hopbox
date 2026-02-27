import type {SidebarsConfig} from '@docusaurus/plugin-content-docs';

const sidebars: SidebarsConfig = {
  docsSidebar: [
    {
      type: 'category',
      label: 'Getting Started',
      items: [
        'getting-started/installation',
        'getting-started/quickstart',
      ],
    },
    {
      type: 'category',
      label: 'Guides',
      items: [
        'guides/setup',
        'guides/workspace-lifecycle',
        'guides/services',
        'guides/bridges',
        'guides/snapshots',
        'guides/migration',
      ],
    },
    {
      type: 'category',
      label: 'Reference',
      items: [
        'reference/cli',
        'reference/manifest',
        'reference/environment',
        'reference/agent-api',
      ],
    },
    {
      type: 'category',
      label: 'Architecture',
      items: [
        'architecture/overview',
        'architecture/wireguard-tunnel',
        'architecture/helper-daemon',
      ],
    },
  ],
};

export default sidebars;
