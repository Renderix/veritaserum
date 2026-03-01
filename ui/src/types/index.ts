export type Protocol = 'HTTP' | 'MYSQL' | 'POSTGRES' | 'REDIS' | 'DYNAMODB'
export type InteractionState = 'pending' | 'configured'

export interface InteractionRequest {
  method?: string
  host?: string
  path?: string
  headers?: Record<string, string>
  body?: string
  bodyHash?: string
  // DynamoDB
  operation?: string
  table?: string
  keyJSON?: string
  // DB
  query?: string
  // Redis
  command?: string
  args?: string[]
}

export interface InteractionResponse {
  // HTTP
  statusCode?: number
  headers?: Record<string, string>
  body?: string
  latencyMs?: number
  // DB
  rows?: Record<string, unknown>[]
  affectedRows?: number
  // DynamoDB
  itemJSON?: string
  // Redis
  value?: string
}

export interface Interaction {
  id: string
  protocol: Protocol
  key: string
  name: string
  request: InteractionRequest
  response?: InteractionResponse
  state: InteractionState
  testCaseId: string
  capturedAt: string
}

export interface TestCase {
  id: string
  name: string
  description?: string
  interactionIds: string[]
  createdAt: string
}

export interface Schema {
  tableName: string
  protocol: 'MYSQL' | 'POSTGRES'
  createStatement: string
}
