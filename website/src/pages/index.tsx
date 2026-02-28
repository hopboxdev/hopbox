import React from 'react';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import styles from './index.module.css';

function Hero() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <header className={styles.hero}>
      <h1 className={styles.heroTitle}>
        Remote development that feels like{' '}
        <span className={styles.heroAccent}>home</span>
      </h1>
      <p className={styles.heroSubtitle}>
        Your VPS, your rules. One command to set up, one to connect. No cloud
        accounts, no seat fees.
      </p>
      <div className={styles.buttons}>
        <Link className="button button--primary button--lg" to="/docs/getting-started/installation">
          Get Started
        </Link>
        <Link className="button button--secondary button--lg" href="https://github.com/hopboxdev/hopbox">
          GitHub
        </Link>
      </div>
      <div className={styles.terminalHero}>
        <div className={styles.terminalWindow}>
          <div className={styles.terminalBar}>
            <span className={styles.terminalDot} style={{background: '#ff5f57'}} />
            <span className={styles.terminalDot} style={{background: '#febc2e'}} />
            <span className={styles.terminalDot} style={{background: '#28c840'}} />
          </div>
          <div className={styles.terminalBody}>
            <div><span className={styles.terminalComment}># Bootstrap your VPS</span></div>
            <div><span className={styles.terminalCommand}>$ hop setup mybox -a 1.2.3.4 -u debian -k ~/.ssh/key</span></div>
            <div><span className={styles.terminalOutput}>  Installing hop-agent... done</span></div>
            <div><span className={styles.terminalOutput}>  Exchanging WireGuard keys... done</span></div>
            <div><span className={styles.terminalOutput}>  Host "mybox" saved as default.</span></div>
            <div className={styles.terminalSpacer} />
            <div><span className={styles.terminalComment}># Bring up the tunnel</span></div>
            <div><span className={styles.terminalCommand}>$ hop up</span></div>
            <div><span className={styles.terminalOutput}>  WireGuard tunnel... up</span></div>
            <div><span className={styles.terminalOutput}>  Agent probe... healthy</span></div>
            <div><span className={styles.terminalOutput}>  Syncing workspace... done</span></div>
            <div><span className={styles.terminalOutput}>  Agent is up.</span></div>
          </div>
        </div>
      </div>
    </header>
  );
}

const features = [
  {
    title: 'WireGuard Tunnel',
    icon: '\u{1F510}',
    description: 'Private L3 network to your VPS. Every port reachable — no SSH forwarding.',
    color: 'violet',
  },
  {
    title: 'Workspace Manifest',
    icon: '\u{1F4E6}',
    description: 'One hopbox.yaml for packages, services, bridges, env, scripts, and backups.',
    color: 'pink',
  },
  {
    title: 'Workspace Mobility',
    icon: '\u{1F680}',
    description: 'Snapshot and migrate to a new host with one command.',
    color: 'indigo',
  },
];

function Features() {
  return (
    <section className={styles.features}>
      <div className={styles.featuresInner}>
        {features.map((feature) => (
          <div key={feature.title} className={`${styles.feature} ${styles[`feature--${feature.color}`]}`}>
            <span className={styles.featureIcon}>{feature.icon}</span>
            <h3 className={styles.featureTitle}>{feature.title}</h3>
            <p className={styles.featureDescription}>{feature.description}</p>
          </div>
        ))}
      </div>
    </section>
  );
}

export default function Home(): React.JSX.Element {
  const {siteConfig} = useDocusaurusContext();
  return (
    <Layout title={siteConfig.title} description={siteConfig.tagline}>
      <Hero />
      <main>
        <Features />
      </main>
    </Layout>
  );
}
