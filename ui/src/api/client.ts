import type { Interaction, InteractionResponse, TestCase, Schema } from '../types'

async function json<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  if (res.status === 204) return undefined as T
  return res.json()
}

export const api = {
  interactions: {
    all: () => json<Interaction[]>('/api/interactions'),
    pending: () => json<Interaction[]>('/api/interactions/pending'),
    configure: (id: string, name: string, response: InteractionResponse) =>
      json<void>(`/api/interactions/${id}/configure`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, response }),
      }),
  },
  testcases: {
    all: () => json<TestCase[]>('/api/testcases'),
    create: (name: string, description?: string) =>
      json<TestCase>('/api/testcases', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, description }),
      }),
    update: (id: string, payload: { name?: string; description?: string; interactionIds?: string[] }) =>
      json<void>(`/api/testcases/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      }),
    delete: (id: string) => json<void>(`/api/testcases/${id}`, { method: 'DELETE' }),
    exportUrl: (id: string) => `/api/testcases/${id}/export`,
  },
  schemas: {
    all: () => json<Schema[]>('/api/schemas'),
    upsert: (protocol: string, tableName: string, createStatement: string) =>
      json<void>('/api/schemas', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ protocol, tableName, createStatement }),
      }),
  },
  import: (file: string) =>
    json<TestCase>('/api/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: file,
    }),
  saveState: () => json<void>('/api/state/save', { method: 'POST' }),
}
