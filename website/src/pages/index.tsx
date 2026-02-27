import React from 'react';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import styles from './index.module.css';

function Hero() {
  const {siteConfig} = useDocusaurusContext();
  return (
    <header className={styles.hero}>
      <h1 className={styles.heroTitle}>{siteConfig.title}</h1>
      <p className={styles.heroSubtitle}>
        Instant dev environments on your own VPS — no cloud accounts,
        no coordination server, no monthly seat fee.
      </p>
      <div className={styles.buttons}>
        <Link className="button button--primary button--lg" to="/docs/getting-started/installation">
          Get Started
        </Link>
        <Link className="button button--secondary button--lg" href="https://github.com/hopboxdev/hopbox">
          GitHub
        </Link>
      </div>
    </header>
  );
}

const features = [
  {
    title: 'WireGuard Tunnel',
    description:
      'A private L3 network between your laptop and VPS. Every port is directly reachable — no per-port SSH forwarding.',
  },
  {
    title: 'Workspace Manifest',
    description:
      'A single hopbox.yaml declares packages, services, bridges, env vars, scripts, backups, and sessions.',
  },
  {
    title: 'Workspace Mobility',
    description:
      'Snapshot your workspace, migrate to a new host with one command. Your data follows you across providers.',
  },
  {
    title: 'Hybrid Services',
    description:
      'Run Docker containers and native processes side by side, with health checks, dependencies, and log aggregation.',
  },
];

function Features() {
  return (
    <section className={styles.features}>
      {features.map((feature) => (
        <div key={feature.title} className={styles.feature}>
          <h3 className={styles.featureTitle}>{feature.title}</h3>
          <p className={styles.featureDescription}>{feature.description}</p>
        </div>
      ))}
    </section>
  );
}

function TerminalDemo() {
  return (
    <section className={styles.terminal}>
      <div className={styles.terminalWindow}>
        <div className={styles.terminalBar}>
          <span className={styles.terminalDot} />
          <span className={styles.terminalDot} />
          <span className={styles.terminalDot} />
        </div>
        <div className={styles.terminalBody}>
          <div><span className={styles.terminalComment}># Bootstrap your VPS</span></div>
          <div><span className={styles.terminalCommand}>$ hop setup mybox -a 1.2.3.4 -u debian -k ~/.ssh/key</span></div>
          <div><span className={styles.terminalOutput}>  Installing hop-agent... done</span></div>
          <div><span className={styles.terminalOutput}>  Exchanging WireGuard keys... done</span></div>
          <div><span className={styles.terminalOutput}>  Host "mybox" saved as default.</span></div>
          <br />
          <div><span className={styles.terminalComment}># Bring up the tunnel</span></div>
          <div><span className={styles.terminalCommand}>$ hop up</span></div>
          <div><span className={styles.terminalOutput}>  WireGuard tunnel... up</span></div>
          <div><span className={styles.terminalOutput}>  Agent probe... healthy</span></div>
          <div><span className={styles.terminalOutput}>  Syncing workspace... done</span></div>
          <div><span className={styles.terminalOutput}>  Agent is up.</span></div>
        </div>
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
        <TerminalDemo />
      </main>
    </Layout>
  );
}
