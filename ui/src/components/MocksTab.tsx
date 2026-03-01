import { useEffect, useState } from 'react'
import { api } from '../api/client'
import type { Interaction, InteractionResponse, Schema } from '../types'
import HttpForm from '../forms/HttpForm'
import MySqlForm from '../forms/MySqlForm'
import PostgresForm from '../forms/PostgresForm'
import DynamoDbForm from '../forms/DynamoDbForm'
import RedisForm from '../forms/RedisForm'

const PROTOCOL_COLORS: Record<string, string> = {
  HTTP: '#0ea5e9',
  MYSQL: '#f59e0b',
  POSTGRES: '#6366f1',
  REDIS: '#ef4444',
  DYNAMODB: '#10b981',
}

export default function MocksTab() {
  const [interactions, setInteractions] = useState<Interaction[]>([])
  const [schemas, setSchemas]           = useState<Schema[]>([])
  const [selected, setSelected]         = useState<string | null>(null)
  const [editing, setEditing]           = useState(false)

  useEffect(() => {
    api.interactions.all()
      .then(list => setInteractions(list.filter(i => i.state === 'configured')))
      .catch(() => {})
    api.schemas.all().then(setSchemas).catch(() => {})
  }, [])

  async function handleSave(id: string, name: string, response: InteractionResponse) {
    await api.interactions.configure(id, name, response)
    const updated = await api.interactions.all()
    setInteractions(updated.filter(i => i.state === 'configured'))
    setEditing(false)
  }

  const active = interactions.find(i => i.id === selected)

  // Group by protocol
  const byProtocol: Record<string, Interaction[]> = {}
  for (const i of interactions) {
    ;(byProtocol[i.protocol] ??= []).push(i)
  }

  return (
    <div style={{ display: 'flex', height: 'calc(100vh - 52px)', gap: 0 }}>
      {/* Left: grouped list */}
      <div style={{ width: 340, borderRight: '1px solid #1e293b', overflowY: 'auto', flexShrink: 0 }}>
        {interactions.length === 0
          ? <div style={{ padding: 24, color: '#475569', fontSize: 13 }}>No configured mocks yet.</div>
          : Object.entries(byProtocol).map(([proto, list]) => (
            <div key={proto}>
              <div style={{
                padding: '6px 16px',
                background: '#0f172a',
                fontSize: 11, fontWeight: 700,
                color: PROTOCOL_COLORS[proto] ?? '#64748b',
                textTransform: 'uppercase', letterSpacing: 1,
              }}>
                {proto}
              </div>
              {list.map(i => (
                <div
                  key={i.id}
                  onClick={() => { setSelected(i.id); setEditing(false) }}
                  style={{
                    padding: '10px 16px', cursor: 'pointer',
                    borderBottom: '1px solid #1e293b',
                    background: selected === i.id ? '#1e293b' : 'transparent',
                  }}
                >
                  <div style={{ fontSize: 13, color: '#e2e8f0', marginBottom: 2 }}>
                    {i.name || i.key}
                  </div>
                  <div style={{ fontSize: 11, color: '#64748b', fontFamily: 'monospace' }}>
                    {i.key.slice(0, 55)}
                  </div>
                </div>
              ))}
            </div>
          ))
        }
      </div>

      {/* Right: detail / edit */}
      <div style={{ flex: 1, padding: 24, overflowY: 'auto' }}>
        {!active
          ? <div style={{ color: '#475569', fontSize: 13 }}>Select a mock to view or edit it.</div>
          : editing
            ? renderForm(active, schemas, (id, name, resp) => handleSave(id, name, resp))
            : <Detail interaction={active} onEdit={() => setEditing(true)} />
        }
      </div>
    </div>
  )
}

function Detail({ interaction: i, onEdit }: { interaction: Interaction; onEdit: () => void }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <h3 style={{ color: '#e2e8f0', fontSize: 15 }}>{i.name || i.key}</h3>
        <button
          onClick={onEdit}
          style={{ background: '#334155', color: '#e2e8f0', border: 'none', padding: '6px 12px', borderRadius: 4, cursor: 'pointer', fontSize: 13 }}
        >
          Edit
        </button>
      </div>
      <pre style={{
        background: '#0f172a', padding: 12, borderRadius: 4,
        fontSize: 12, color: '#94a3b8', overflowX: 'auto',
        whiteSpace: 'pre-wrap', wordBreak: 'break-all',
      }}>
        {JSON.stringify(i.response, null, 2)}
      </pre>
    </div>
  )
}

function renderForm(
  i: Interaction,
  schemas: Schema[],
  onSave: (id: string, name: string, resp: InteractionResponse) => void
) {
  const save = (name: string, resp: InteractionResponse) => onSave(i.id, name, resp)
  switch (i.protocol) {
    case 'HTTP':
      return <HttpForm interaction={i} onSave={save} />
    case 'MYSQL': {
      const schema = schemas.find(s => s.protocol === 'MYSQL' && i.request.query?.includes(s.tableName))
      return <MySqlForm interaction={i} existingSchema={schema?.createStatement} onSave={save} onSaveSchema={() => {}} />
    }
    case 'POSTGRES': {
      const schema = schemas.find(s => s.protocol === 'POSTGRES' && i.request.query?.includes(s.tableName))
      return <PostgresForm interaction={i} existingSchema={schema?.createStatement} onSave={save} onSaveSchema={() => {}} />
    }
    case 'DYNAMODB':
      return <DynamoDbForm interaction={i} onSave={save} />
    case 'REDIS':
      return <RedisForm interaction={i} onSave={save} />
    default:
      return null
  }
}
