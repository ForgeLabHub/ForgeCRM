/**
 * MCP client credentials.
 *
 * A credential lets an external MCP client reach the CRM without a browser
 * session. It is deliberately narrower than its owner: it carries an explicit
 * permission list, and a tool call is authorized against that list, not the
 * owner's roles.
 *
 * The plaintext token appears exactly once, in the create and rotate responses,
 * and is never returned by the list. That is why the page shows it in a one-time
 * dialog and never stores it.
 */
import { api, type ListResponse } from './client'

export type MCPCredentialStatus = 'active' | 'revoked' | 'expired'

/** A stored credential, without the secret. */
export interface MCPCredential {
  id: string
  organization_id: string
  owner_user_id: string
  name: string
  token_prefix: string
  permissions: string[]
  expires_at?: string
  revoked_at?: string
  last_used_at?: string
  created_at: string
  updated_at: string
  status: MCPCredentialStatus
}

/** The one-time result of minting or rotating a credential. */
export interface MCPCredentialCreated {
  credential: MCPCredential
  token: string
}

export interface MCPCredentialInput {
  name: string
  permissions: string[]
  /** Zero or absent means the credential does not expire. */
  expires_in_hours?: number
}

export const mcpCredentialsApi = {
  list: () => api.get<ListResponse<MCPCredential>>('/api/v1/mcp/credentials'),
  create: (body: MCPCredentialInput) =>
    api.post<MCPCredentialCreated>('/api/v1/mcp/credentials', body),
  rotate: (id: string) => api.post<MCPCredentialCreated>(`/api/v1/mcp/credentials/${id}/rotate`),
  revoke: (id: string) => api.delete(`/api/v1/mcp/credentials/${id}`),
}
