/**
 * Tests for the custom-fields panel.
 *
 * The panel is the only interface to custom-field definitions and values, which
 * the server already implements. The assertions are about what it sends and what
 * it exposes: a value change hands the whole record map back with the one key
 * changed, a user without the record's update permission sees a read-only view,
 * and a definition is only offered to a user who may manage them.
 */
import { describe, expect, it, vi, beforeEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { CustomFieldsPanel } from './CustomFieldsPanel'
import { AuthProvider } from '../hooks/useAuth'
import type { CustomFields } from '../types/api'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

interface StubOptions {
  permissions?: string[]
  definitions?: unknown[]
  onCreate?: (url: string, body: unknown) => void
}

function stubFetch(options: StubOptions = {}) {
  const permissions = options.permissions ?? [
    'contacts.read',
    'accounts.update',
    'custom_fields.manage',
  ]
  const fetchMock = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'
    if (url.includes('/api/v1/auth/me')) {
      return Promise.resolve(
        json({
          user: { id: 'u1', email: 'a@example.com', display_name: 'A' },
          organization: { id: 'o1', name: 'Org' },
          permissions,
          csrf_token: 't',
        }),
      )
    }
    if (url.includes('/api/v1/custom-fields')) {
      if (method === 'POST') {
        options.onCreate?.(url, JSON.parse(String(init?.body ?? '{}')))
        return Promise.resolve(json({ id: 'new' }, 201))
      }
      return Promise.resolve(json({ items: options.definitions ?? [] }))
    }
    return Promise.resolve(json({}))
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderPanel(
  values: CustomFields = {},
  canEdit = true,
  onSave: (next: CustomFields) => Promise<unknown> = () => Promise.resolve({}),
) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <CustomFieldsPanel
          entityType="account"
          values={values}
          canEdit={canEdit}
          onSave={onSave}
        />
      </AuthProvider>
    </QueryClientProvider>,
  )
}

const TEXT_FIELD = {
  id: 'f1',
  entity_type: 'account',
  key: 'region_code',
  label: 'Region code',
  field_type: 'text',
  required: false,
  position: 0,
  created_at: 'x',
}

beforeEach(() => {
  vi.unstubAllGlobals()
})

describe('CustomFieldsPanel', () => {
  it('renders the value a record already carries', async () => {
    stubFetch({ definitions: [TEXT_FIELD] })
    renderPanel({ region_code: 'EMEA' })

    const input = await screen.findByLabelText('Region code')
    expect(input).toHaveValue('EMEA')
  })

  it('saves the merged map with the one changed key', async () => {
    const saved: CustomFields[] = []
    stubFetch({ definitions: [TEXT_FIELD] })
    renderPanel({ region_code: 'EMEA', untouched: 'keep' }, true, (next) => {
      saved.push(next)
      return Promise.resolve({})
    })

    const input = await screen.findByLabelText('Region code')
    await userEvent.clear(input)
    await userEvent.type(input, 'APAC')
    await userEvent.tab()

    await waitFor(() => expect(saved).toHaveLength(1))
    expect(saved[0]).toEqual({ region_code: 'APAC', untouched: 'keep' })
  })

  it('shows a read-only value to a user who cannot edit the record', async () => {
    stubFetch({ definitions: [TEXT_FIELD], permissions: ['contacts.read'] })
    renderPanel({ region_code: 'EMEA' }, false)

    expect(await screen.findByText('EMEA')).toBeInTheDocument()
    expect(screen.queryByLabelText('Region code')).not.toBeInTheDocument()
    // And no definition form without custom_fields.manage.
    expect(screen.queryByLabelText('Field key')).not.toBeInTheDocument()
  })

  it('lets a manager define a field and sends the parsed choices', async () => {
    const writes: { url: string; body: unknown }[] = []
    stubFetch({ definitions: [], onCreate: (url, body) => writes.push({ url, body }) })
    renderPanel()

    await userEvent.type(await screen.findByLabelText('Field key'), 'Tier')
    await userEvent.type(screen.getByLabelText('Field label'), 'Account tier')
    await userEvent.selectOptions(screen.getByLabelText('Field type'), 'select')
    await userEvent.type(screen.getByLabelText('Choices'), 'Gold, Silver, Bronze')
    await userEvent.click(screen.getByRole('button', { name: 'Add field' }))

    await waitFor(() => expect(writes).toHaveLength(1))
    expect(writes[0].body).toEqual({
      entity_type: 'account',
      key: 'tier',
      label: 'Account tier',
      field_type: 'select',
      options: { choices: ['Gold', 'Silver', 'Bronze'] },
    })
  })

  it('renders nothing when there are no fields and no permission to add one', async () => {
    stubFetch({ definitions: [], permissions: ['contacts.read'] })
    const { container } = renderPanel({}, false)

    await waitFor(() => expect(container).toBeEmptyDOMElement())
  })
})
