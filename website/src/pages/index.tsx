import React, {useState} from 'react';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';
import styles from './index.module.css';

const INSTALL_CMD = 'curl -fsSL https://get.hopbox.dev | sh';

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
      <Install />
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

function Install() {
  const [copied, setCopied] = useState(false);
  const handleCopy = () => {
    navigator.clipboard.writeText(INSTALL_CMD);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };
  return (
    <div className={styles.install}>
      <div className={styles.installCode}>
        <code>{INSTALL_CMD}</code>
        <button
          className={styles.copyButton}
          onClick={handleCopy}
          aria-label="Copy to clipboard"
          type="button"
        >
          {copied ? (
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polyline points="20 6 9 17 4 12" /></svg>
          ) : (
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2" /><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" /></svg>
          )}
        </button>
      </div>
      <p className={styles.installAlt}>
        or <code>brew install hopboxdev/tap/hop</code>
      </p>
    </div>
  );
}

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
