import { useState } from 'react'
import type { Interaction, InteractionResponse } from '../types'

interface Props {
  interaction: Interaction
  existingSchema?: string
  onSave: (name: string, response: InteractionResponse) => void
  onSaveSchema: (tableName: string, createStatement: string) => void
}

export default function MySqlForm({ interaction: i, existingSchema, onSave, onSaveSchema }: Props) {
  const isSelect = i.request.query?.trim().toUpperCase().startsWith('SELECT') ?? false
  const [name, setName]             = useState(i.name || '')
  const [rows, setRows]             = useState(i.response?.rows ? JSON.stringify(i.response.rows, null, 2) : '[]')
  const [affectedRows, setAffected] = useState(i.response?.affectedRows ?? 1)
  const [schema, setSchema]         = useState(existingSchema ?? '')
  const [tableName, setTableName]   = useState('')
  const needsSchema = !existingSchema

  function handleSave() {
    if (needsSchema && tableName && schema) {
      onSaveSchema(tableName, schema)
    }
    const response: InteractionResponse = isSelect
      ? { rows: JSON.parse(rows) }
      : { affectedRows }
    onSave(name, response)
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <ReadOnly label="SQL query" value={i.request.query ?? ''} />
      {needsSchema && (
        <>
          <Field label="Table name" value={tableName} onChange={setTableName} />
          <TextArea label="CREATE TABLE statement" value={schema} onChange={setSchema} rows={4} />
        </>
      )}
      <Field label="Name / label" value={name} onChange={setName} />
      {isSelect
        ? <TextArea label="Rows to return (JSON array)" value={rows} onChange={setRows} rows={8} />
        : <NumberField label="Affected rows" value={affectedRows} onChange={setAffected} />
      }
      <SaveButton onClick={handleSave} />
    </div>
  )
}

function ReadOnly({ label, value }: { label: string; value: string }) {
  return (
    <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
      <small style={{ color: '#94a3b8' }}>{label}</small>
      <div style={{ background: '#0f172a', border: '1px solid #1e293b', color: '#7c3aed', padding: '6px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12 }}>{value}</div>
    </label>
  )
}
function Field({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  return <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}><small style={{ color: '#94a3b8' }}>{label}</small><input value={value} onChange={e => onChange(e.target.value)} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }} /></label>
}
function NumberField({ label, value, onChange }: { label: string; value: number; onChange: (v: number) => void }) {
  return <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}><small style={{ color: '#94a3b8' }}>{label}</small><input type="number" value={value} onChange={e => onChange(Number(e.target.value))} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4 }} /></label>
}
function TextArea({ label, value, onChange, rows }: { label: string; value: string; onChange: (v: string) => void; rows: number }) {
  return <label style={{ display: 'flex', flexDirection: 'column', gap: 4 }}><small style={{ color: '#94a3b8' }}>{label}</small><textarea rows={rows} value={value} onChange={e => onChange(e.target.value)} style={{ background: '#1e293b', border: '1px solid #334155', color: '#e2e8f0', padding: '6px 8px', borderRadius: 4, fontFamily: 'monospace', fontSize: 12, resize: 'vertical' }} /></label>
}
function SaveButton({ onClick }: { onClick: () => void }) {
  return <button onClick={onClick} style={{ background: '#7c3aed', color: '#fff', border: 'none', padding: '8px 16px', borderRadius: 4, cursor: 'pointer', fontWeight: 600, alignSelf: 'flex-start' }}>Save mock</button>
}
