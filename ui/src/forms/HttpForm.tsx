import { useState } from 'react'
import type { Interaction, InteractionResponse } from '../types'

interface Props {
  interaction: Interaction
  onSave: (name: string, response: InteractionResponse) => void
}

export default function HttpForm({ interaction: i, onSave }: Props) {
  const [name, setName]          = useState(i.name || '')
  const [statusCode, setStatus]  = useState(i.response?.statusCode ?? 200)
  const [latencyMs, setLatency]  = useState(i.response?.latencyMs ?? 0)
  const [body, setBody]          = useState(i.response?.body ?? '')
  const [headersRaw, setHeaders] = useState(
    i.response?.headers ? JSON.stringify(i.response.headers, null, 2) : '{}'
  )

  function handleSave() {
    let headers: Record<string, string> = {}
    try { headers = JSON.parse(headersRaw) } catch { /* ignore bad JSON */ }
    onSave(name, { statusCode, latencyMs, body, headers })
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ fontFamily: 'monospace', fontSize: 13, color: '#7c3aed' }}>
        {i.request.method} {i.request.host}{i.request.path}
      </div>
      <Field label="Name / label" value={name} onChange={setName} />
      <div style={{ display: 'flex', gap: 12 }}>
        <NumberField label="Status code" value={statusCode} onChange={setStatus} />
        <NumberField label="Latency (ms)" value={latencyMs} onChange={setLatency} />
      </div>
      <TextArea label="Response headers (JSON)" value={headersRaw} onChange={setHeaders} rows={3} />
      <TextArea label="Response body" value={body} onChange={setBody} rows={8} />
      <SaveButton onClick={handleSave} />
    </div>
  )
}

function Field({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <small style={{ color: '#94a3b8' }}>{label}</small>
      <input value={value} onChange={e => onChange(e.target.value)}
        style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }} />
    </label>
  )
}

function NumberField({ label, value, onChange }: { label: string; value: number; onChange: (v: number) => void }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4, flex: 1 }}>
      <small style={{ color: '#94a3b8' }}>{label}</small>
      <input type="number" value={value} onChange={e => onChange(Number(e.target.value))}
        style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }} />
    </label>
  )
}

function TextArea({ label, value, onChange, rows }: { label: string; value: string; onChange: (v: string) => void; rows: number }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <small style={{ color: '#94a3b8' }}>{label}</small>
      <textarea rows={rows} value={value} onChange={e => onChange(e.target.value)}
        style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12, resize: 'vertical' }} />
    </label>
  )
}

function SaveButton({ onClick }: { onClick: () => void }) {
  return (
    <button onClick={onClick}
      style={{ background: '#7c3aed', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: 4, cursor: 'pointer', fontWeight: 600, alignSelf: 'flex-start' }}>
      Save mock
    </button>
  )
}
