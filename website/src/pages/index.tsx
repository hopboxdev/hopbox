import React from 'react';
import Layout from '@theme/Layout';

export default function Home(): React.JSX.Element {
  return (
    <Layout title="Hopbox" description="Instant dev environments on your own VPS">
      <main style={{padding: '4rem 2rem', textAlign: 'center'}}>
        <h1>Hopbox</h1>
        <p>Instant dev environments on your own VPS</p>
      </main>
    </Layout>
  );
}
