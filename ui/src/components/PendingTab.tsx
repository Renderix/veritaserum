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

export default function PendingTab() {
  const [interactions, setInteractions] = useState<Interaction[]>([])
  const [schemas, setSchemas]           = useState<Schema[]>([])
  const [selected, setSelected]         = useState<string | null>(null)

  useEffect(() => {
    const load = () => {
      api.interactions.pending().then(setInteractions).catch(() => {})
      api.schemas.all().then(setSchemas).catch(() => {})
    }
    load()
    const t = setInterval(load, 2000)
    return () => clearInterval(t)
  }, [])

  async function handleSave(id: string, name: string, response: InteractionResponse) {
    await api.interactions.configure(id, name, response)
    setInteractions(prev => prev.filter(i => i.id !== id))
    setSelected(null)
  }

  async function handleSaveSchema(tableName: string, createStatement: string, protocol: string) {
    await api.schemas.upsert(protocol, tableName, createStatement)
    const updated = await api.schemas.all()
    setSchemas(updated)
  }

  const active = interactions.find(i => i.id === selected)

  return (
    <div style={{ display: 'flex', height: 'calc(100vh - 52px)', gap: 0 }}>
      {/* Left: list */}
      <div style={{ width: 340, borderRight: '1px solid #1e293b', overflowY: 'auto', flexShrink: 0 }}>
        {interactions.length === 0
          ? <div style={{ padding: 24, color: '#475569', fontSize: 13 }}>
              No pending interactions. Proxy some traffic to see captures here.
            </div>
          : interactions.map(i => (
            <div
              key={i.id}
              onClick={() => setSelected(i.id)}
              style={{
                padding: '12px 16px', cursor: 'pointer',
                borderBottom: '1px solid #1e293b',
                background: selected === i.id ? '#1e293b' : 'transparent',
              }}
            >
              <div style={{ display: 'flex', gap: 8, alignItems: 'center', marginBottom: 4 }}>
                <span style={{
                  background: PROTOCOL_COLORS[i.protocol] ?? '#64748b',
                  color: '#fff', fontSize: 10, fontWeight: 700,
                  padding: '2px 6px', borderRadius: 3,
                }}>
                  {i.protocol}
                </span>
                <span style={{ fontSize: 12, fontFamily: 'monospace', color: '#94a3b8' }}>
                  {i.protocol === 'HTTP' || i.protocol === 'DYNAMODB'
                    ? `${i.request.method} ${i.request.host}`
                    : (i.request.query ?? i.request.command ?? i.key).slice(0, 40)
                  }
                </span>
              </div>
              {(i.protocol === 'HTTP' || i.protocol === 'DYNAMODB') &&
                <div style={{ fontSize: 11, color: '#64748b', fontFamily: 'monospace' }}>
                  {i.request.path}
                </div>
              }
            </div>
          ))
        }
      </div>

      {/* Right: form */}
      <div style={{ flex: 1, padding: 24, overflowY: 'auto' }}>
        {!active
          ? <div style={{ color: '#475569', fontSize: 13 }}>
              Select an interaction to configure its mock response.
            </div>
          : renderForm(active, schemas, handleSave, handleSaveSchema)
        }
      </div>
    </div>
  )
}

function renderForm(
  i: Interaction,
  schemas: Schema[],
  onSave: (id: string, name: string, resp: InteractionResponse) => void,
  onSaveSchema: (tableName: string, createStatement: string, protocol: string) => void
) {
  const save = (name: string, resp: InteractionResponse) => onSave(i.id, name, resp)

  switch (i.protocol) {
    case 'HTTP':
      return <HttpForm interaction={i} onSave={save} />
    case 'MYSQL': {
      const schema = schemas.find(s => s.protocol === 'MYSQL' && i.request.query?.includes(s.tableName))
      return (
        <MySqlForm
          interaction={i}
          existingSchema={schema?.createStatement}
          onSave={save}
          onSaveSchema={(t, c) => onSaveSchema(t, c, 'MYSQL')}
        />
      )
    }
    case 'POSTGRES': {
      const schema = schemas.find(s => s.protocol === 'POSTGRES' && i.request.query?.includes(s.tableName))
      return (
        <PostgresForm
          interaction={i}
          existingSchema={schema?.createStatement}
          onSave={save}
          onSaveSchema={(t, c) => onSaveSchema(t, c, 'POSTGRES')}
        />
      )
    }
    case 'DYNAMODB':
      return <DynamoDbForm interaction={i} onSave={save} />
    case 'REDIS':
      return <RedisForm interaction={i} onSave={save} />
    default:
      return <div style={{ color: '#ef4444' }}>Unknown protocol: {i.protocol}</div>
  }
}
