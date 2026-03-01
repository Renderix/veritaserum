import { useState } from 'react'
import { api } from '../api/client'
import type { TestCase } from '../types'

export default function ImportTab() {
  const [content, setContent] = useState('')
  const [result, setResult]   = useState<TestCase | null>(null)
  const [error, setError]     = useState('')

  async function handleImport() {
    setError('')
    setResult(null)
    try {
      const tc = await api.import(content)
      setResult(tc)
    } catch (e) {
      setError(String(e))
    }
  }

  function handleFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = ev => setContent(ev.target?.result as string ?? '')
    reader.readAsText(file)
  }

  return (
    <div style={{ maxWidth: 680, padding: '24px 0', display: 'flex', flexDirection: 'column', gap: 16 }}>
      <h3 style={{ color: '#e2e8f0' }}>Import a test suite</h3>
      <p style={{ color: '#94a3b8', fontSize: 13 }}>
        Load a previously exported <code style={{ background: '#1e293b', padding: '1px 4px', borderRadius: 3 }}>.json</code> file
        to restore its test case and interactions.
      </p>
      <input type="file" accept=".json" onChange={handleFile} style={{ color: '#94a3b8', fontSize: 13 }} />
      <textarea
        rows={16}
        value={content}
        onChange={e => setContent(e.target.value)}
        placeholder="…or paste the JSON here"
        style={{
          background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0',
          padding: 8, borderRadius: 4, fontFamily: 'monospace', fontSize: 12, resize: 'vertical',
        }}
      />
      <button
        onClick={handleImport}
        disabled={!content.trim()}
        style={{
          background: content.trim() ? '#7c3aed' : '#334155',
          color: '#fff', border: 'none', padding: '8px 16px',
          borderRadius: 4, cursor: content.trim() ? 'pointer' : 'default',
          fontWeight: 600, alignSelf: 'flex-start',
        }}
      >
        Import
      </button>
      {result && (
        <div style={{ color: '#10b981', fontSize: 13 }}>
          Imported: <strong>{result.name}</strong> ({result.interactionIds.length} interactions)
        </div>
      )}
      {error && <div style={{ color: '#ef4444', fontSize: 13 }}>{error}</div>}
    </div>
  )
}
