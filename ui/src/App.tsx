import { useState } from 'react'

type Tab = 'pending' | 'mocks' | 'testcases' | 'import'

export default function App() {
  const [tab, setTab] = useState<Tab>('pending')

  return (
    <div style={{ minHeight: '100vh', display: 'flex', flexDirection: 'column' }}>
      <header style={{ background: '#1e293b', padding: '0 24px', display: 'flex', gap: 24, alignItems: 'center', height: 52, borderBottom: '1px solid #0f172a', flexShrink: 0 }}>
        <span style={{ fontWeight: 700, fontSize: 16, color: '#7c3aed', marginRight: 8 }}>⚗ Veritaserum</span>
        {(['pending', 'mocks', 'testcases', 'import'] as Tab[]).map(t => (
          <button
            key={t}
            onClick={() => setTab(t)}
            style={{
              background: 'none', border: 'none', cursor: 'pointer',
              color: tab === t ? '#a78bfa' : '#94a3b8',
              fontWeight: tab === t ? 700 : 400,
              fontSize: 14, padding: '0 4px', height: '100%',
              borderBottom: tab === t ? '2px solid #7c3aed' : '2px solid transparent',
            }}
          >
            {t === 'testcases' ? 'Test Cases' : t.charAt(0).toUpperCase() + t.slice(1)}
          </button>
        ))}
      </header>
      <main style={{ flex: 1, padding: 24 }}>
        <div style={{ color: '#475569' }}>
          {tab === 'pending' && 'Pending tab — coming soon'}
          {tab === 'mocks' && 'Mocks tab — coming soon'}
          {tab === 'testcases' && 'Test Cases tab — coming soon'}
          {tab === 'import' && 'Import tab — coming soon'}
        </div>
      </main>
    </div>
  )
}
