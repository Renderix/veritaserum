import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { TestCase, Interaction } from '../types'

export default function TestCasesTab() {
  const [testCases, setTestCases]       = useState<TestCase[]>([])
  const [interactions, setInteractions] = useState<Interaction[]>([])
  const [selected, setSelected]         = useState<string | null>(null)
  const [newName, setNewName]           = useState('')
  const [newDesc, setNewDesc]           = useState('')
  const [creating, setCreating]         = useState(false)

  useEffect(() => {
    api.testcases.all().then(setTestCases).catch(() => {})
    api.interactions.all()
      .then(list => setInteractions(list.filter(i => i.state === 'configured')))
      .catch(() => {})
  }, [])

  async function handleCreate() {
    if (!newName.trim()) return
    const tc = await api.testcases.create(newName.trim(), newDesc.trim() || undefined)
    setTestCases(prev => [...prev, tc])
    setNewName('')
    setNewDesc('')
    setCreating(false)
    setSelected(tc.id)
  }

  async function handleDelete(id: string) {
    await api.testcases.delete(id)
    setTestCases(prev => prev.filter(tc => tc.id !== id))
    if (selected === id) setSelected(null)
  }

  async function toggleInteraction(tcId: string, iid: string, currentIds: string[]) {
    const next = currentIds.includes(iid)
      ? currentIds.filter(x => x !== iid)
      : [...currentIds, iid]
    await api.testcases.update(tcId, { interactionIds: next })
    setTestCases(prev => prev.map(tc => tc.id === tcId ? { ...tc, interactionIds: next } : tc))
  }

  const activeTc = testCases.find(tc => tc.id === selected)

  return (
    <div style={{ display: 'flex', height: 'calc(100vh - 52px)', gap: 0 }}>
      {/* Left: test case list */}
      <div style={{ width: 280, borderRight: '1px solid #1e293b', display: 'flex', flexDirection: 'column', flexShrink: 0 }}>
        <div style={{ padding: 12, borderBottom: '1px solid #1e293b' }}>
          <button
            onClick={() => setCreating(true)}
            style={{ width: '100%', background: '#7c3aed', color: '#fff', border: 'none', padding: '7px 10px', borderRadius: 4, cursor: 'pointer', fontSize: 13, fontWeight: 600 }}
          >
            + New test case
          </button>
        </div>
        <div style={{ flex: 1, overflowY: 'auto' }}>
          {testCases.map(tc => (
            <div
              key={tc.id}
              onClick={() => setSelected(tc.id)}
              style={{
                padding: '10px 16px', cursor: 'pointer',
                borderBottom: '1px solid #1e293b',
                background: selected === tc.id ? '#1e293b' : 'transparent',
                display: 'flex', justifyContent: 'space-between', alignItems: 'center',
              }}
            >
              <div>
                <div style={{ fontSize: 13, color: '#e2e8f0' }}>{tc.name}</div>
                <div style={{ fontSize: 11, color: '#64748b' }}>{tc.interactionIds.length} interactions</div>
              </div>
              <button
                onClick={e => { e.stopPropagation(); handleDelete(tc.id) }}
                style={{ background: 'none', border: 'none', color: '#ef4444', cursor: 'pointer', fontSize: 18, lineHeight: 1 }}
              >
                ×
              </button>
            </div>
          ))}
        </div>
        <div style={{ padding: 12, borderTop: '1px solid #1e293b' }}>
          <button
            onClick={() => api.saveState().catch(() => {})}
            style={{ width: '100%', background: '#1e293b', color: '#94a3b8', border: '1px solid #334155', padding: '6px 10px', borderRadius: 4, cursor: 'pointer', fontSize: 12 }}
          >
            Save state to disk
          </button>
        </div>
      </div>

      {/* Right: detail */}
      <div style={{ flex: 1, padding: 24, overflowY: 'auto' }}>
        {creating && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12, maxWidth: 420, marginBottom: 24, padding: 16, background: '#1e293b', borderRadius: 6 }}>
            <h3 style={{ color: '#e2e8f0', fontSize: 14 }}>New test case</h3>
            <input
              placeholder="Name"
              value={newName}
              onChange={e => setNewName(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && handleCreate()}
              style={{ background: '#0f172a', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }}
            />
            <input
              placeholder="Description (optional)"
              value={newDesc}
              onChange={e => setNewDesc(e.target.value)}
              style={{ background: '#0f172a', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }}
            />
            <div style={{ display: 'flex', gap: 8 }}>
              <button onClick={handleCreate} style={{ background: '#7c3aed', color: '#fff', border: 'none', padding: '6px 14px', borderRadius: 4, cursor: 'pointer', fontWeight: 600 }}>Create</button>
              <button onClick={() => setCreating(false)} style={{ background: '#334155', color: '#e2e8f0', border: 'none', padding: '6px 14px', borderRadius: 4, cursor: 'pointer' }}>Cancel</button>
            </div>
          </div>
        )}
        {!activeTc
          ? <div style={{ color: '#475569', fontSize: 13 }}>Select or create a test case.</div>
          : <TestCaseDetail tc={activeTc} interactions={interactions} onToggle={toggleInteraction} />
        }
      </div>
    </div>
  )
}

function TestCaseDetail({
  tc,
  interactions,
  onToggle,
}: {
  tc: TestCase
  interactions: Interaction[]
  onToggle: (tcId: string, iid: string, current: string[]) => void
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, maxWidth: 680 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h3 style={{ color: '#e2e8f0' }}>{tc.name}</h3>
        <a
          href={`/api/testcases/${tc.id}/export`}
          download
          style={{ background: '#0ea5e9', color: '#fff', padding: '6px 12px', borderRadius: 4, textDecoration: 'none', fontSize: 13, fontWeight: 600 }}
        >
          Export JSON
        </a>
      </div>
      {tc.description && <p style={{ color: '#94a3b8', fontSize: 13 }}>{tc.description}</p>}
      <div>
        <div style={{ color: '#94a3b8', fontSize: 11, textTransform: 'uppercase', letterSpacing: 1, marginBottom: 8 }}>
          Interactions ({tc.interactionIds.length})
        </div>
        {interactions.length === 0
          ? <div style={{ color: '#475569', fontSize: 13 }}>No configured mocks yet. Configure some in the Pending tab first.</div>
          : interactions.map(i => (
            <div
              key={i.id}
              style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '8px 0', borderBottom: '1px solid #1e293b' }}
            >
              <input
                type="checkbox"
                checked={tc.interactionIds.includes(i.id)}
                onChange={() => onToggle(tc.id, i.id, tc.interactionIds)}
                style={{ cursor: 'pointer', accentColor: '#7c3aed' }}
              />
              <span style={{ fontSize: 11, fontWeight: 700, color: '#64748b', minWidth: 80 }}>{i.protocol}</span>
              <span style={{ fontSize: 13, color: '#e2e8f0' }}>{i.name || i.key}</span>
            </div>
          ))
        }
      </div>
    </div>
  )
}
