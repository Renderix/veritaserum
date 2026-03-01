import { useState } from 'react'
import type { Interaction, InteractionResponse } from '../types'

interface Props {
  interaction: Interaction
  onSave: (name: string, response: InteractionResponse) => void
}

export default function RedisForm({ interaction: i, onSave }: Props) {
  const [name, setName]   = useState(i.name || '')
  const [value, setValue] = useState(i.response?.value ?? '')

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ fontFamily: 'monospace', fontSize: 13, color: '#7c3aed' }}>
        {i.request.command} {(i.request.args ?? []).join(' ')}
      </div>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <small style={{ color: '#94a3b8' }}>Name / label</small>
        <input value={name} onChange={e => setName(e.target.value)}
          style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }} />
      </label>
      <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <small style={{ color: '#94a3b8' }}>Return value</small>
        <textarea rows={4} value={value} onChange={e => setValue(e.target.value)}
          style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12, resize: 'vertical' }} />
      </label>
      <button onClick={() => onSave(name, { value })}
        style={{ background: '#7c3aed', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: 4, cursor: 'pointer', fontWeight: 600, alignSelf: 'flex-start' }}>
        Save mock
      </button>
    </div>
  )
}
