import { useState } from 'react'
import type { Interaction, InteractionResponse } from '../types'

interface Props {
  interaction: Interaction
  onSave: (name: string, response: InteractionResponse) => void
}

export default function DynamoDbForm({ interaction: i, onSave }: Props) {
  const [name, setName]     = useState(i.name || '')
  const [itemJSON, setItem] = useState(i.response?.itemJSON ?? '{}')

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <ReadOnly label="Operation" value={`${i.request.operation ?? 'Unknown'} on ${i.request.table ?? 'unknown table'}`} />
      <ReadOnly label="Key" value={i.request.keyJSON ?? ''} />
      <Field label="Name / label" value={name} onChange={setName} />
      <TextArea label="Item JSON to return" value={itemJSON} onChange={setItem} rows={10} />
      <button onClick={() => onSave(name, { itemJSON })}
        style={{ background: '#7c3aed', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: 4, cursor: 'pointer', fontWeight: 600, alignSelf: 'flex-start' }}>
        Save mock
      </button>
    </div>
  )
}

function ReadOnly({ label, value }: { label: string; value: string }) {
  return <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}><small style={{ color: '#94a3b8' }}>{label}</small><div style={{ background: '#0f172a', border: '1px solid #1e293b', color: '#7c3aed', padding: '6px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12 }}>{value}</div></label>
}
function Field({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  return <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}><small style={{ color: '#94a3b8' }}>{label}</small><input value={value} onChange={e => onChange(e.target.value)} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }} /></label>
}
function TextArea({ label, value, onChange, rows }: { label: string; value: string; onChange: (v: string) => void; rows: number }) {
  return <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}><small style={{ color: '#94a3b8' }}>{label}</small><textarea rows={rows} value={value} onChange={e => onChange(e.target.value)} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12, resize: 'vertical' }} /></label>
}
