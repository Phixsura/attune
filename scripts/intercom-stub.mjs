#!/usr/bin/env node
// SPDX-License-Identifier: Apache-2.0
//
// Deterministic Intercom API stub for local E2E verification of the
// Intercom inbound adapter (#230). Serves /me, POST /conversations/search,
// GET /conversations/{id} (display_as=plaintext semantics baked into the
// fixture bodies), and POST /contacts/search.
//
// Auth contract: requires "Authorization: Bearer stub-token"; anything
// else gets Intercom's 401 error.list envelope — this exercises the
// adapter's permanent-error path from the real wire shape.
//
// Usage: node scripts/intercom-stub.mjs [port]

import { createServer } from 'node:http'

const port = Number(process.argv[2] ?? 9911)

const NOW = Math.floor(Date.now() / 1000)

const conversations = [
  {
    type: 'conversation',
    id: '9001',
    title: 'Cannot export dashboard as PDF',
    state: 'open',
    priority: 'priority',
    created_at: NOW - 7200,
    updated_at: NOW - 3600,
    admin_assignee_id: 7,
    team_assignee_id: 3,
    ai_agent_participated: false,
    custom_attributes: { plan_tier: 'team', error_code: 'EXPORT_403' },
    source: {
      type: 'conversation',
      subject: '',
      body: 'The PDF export button is greyed out on the Team plan. This blocks our Monday reporting.',
      url: 'https://app.customer.example/dashboards/42',
      author: {
        type: 'user',
        id: 'contact-alice',
        name: 'Alice Zhang',
        email: 'alice@customer.example',
      },
    },
    contacts: {
      type: 'contact.list',
      contacts: [{ type: 'contact', id: 'contact-alice', external_id: 'cust-70' }],
    },
    tags: {
      type: 'tag.list',
      tags: [
        { type: 'tag', id: 't1', name: 'export' },
        { type: 'tag', id: 't2', name: 'bug' },
      ],
    },
    conversation_rating: null,
    company: { type: 'company', id: 'co-9', name: 'Customer Co' },
  },
  {
    type: 'conversation',
    id: '9002',
    title: '',
    state: 'closed',
    priority: '',
    created_at: NOW - 5400,
    updated_at: NOW - 1800,
    admin_assignee_id: 0,
    team_assignee_id: 0,
    ai_agent_participated: true,
    ai_agent: {
      source_type: 'workflow',
      resolution_state: 'escalated',
      last_answer_type: 'ai_answer',
      rating: 2,
      rating_remark: 'did not solve it',
    },
    source: {
      type: 'email',
      subject: 'Feature request: dark mode',
      body: 'Please add dark mode to the mobile app. My eyes hurt at night.',
      author: { type: 'lead', id: 'contact-bob', name: 'Bob Lee', email: 'bob@lead.example' },
    },
    contacts: {
      type: 'contact.list',
      contacts: [{ type: 'contact', id: 'contact-bob', external_id: '' }],
    },
    tags: { type: 'tag.list', tags: [{ type: 'tag', id: 't3', name: 'feature-request' }] },
    conversation_rating: { rating: 5, remark: '' },
    company: null,
  },
  {
    type: 'conversation',
    id: '9003',
    title: 'Spam-tagged conversation (must be filtered when exclude=spam)',
    state: 'open',
    priority: '',
    created_at: NOW - 4000,
    updated_at: NOW - 900,
    admin_assignee_id: 0,
    team_assignee_id: 0,
    ai_agent_participated: false,
    source: {
      type: 'conversation',
      subject: '',
      body: 'Buy cheap watches!!!',
      author: { type: 'lead', id: 'contact-spam', name: 'Spammy', email: 'spam@example.com' },
    },
    contacts: {
      type: 'contact.list',
      contacts: [{ type: 'contact', id: 'contact-spam', external_id: '' }],
    },
    tags: { type: 'tag.list', tags: [{ type: 'tag', id: 't9', name: 'spam' }] },
    conversation_rating: null,
    company: null,
  },
]

const partsByConversation = {
  9001: [
    {
      type: 'conversation_part',
      id: 'p1',
      part_type: 'comment',
      body: 'Thanks for reporting! Which browser are you using?',
      created_at: NOW - 7000,
      author: { type: 'admin', id: 'admin-1', name: 'Sam Support' },
      redacted: false,
    },
    {
      type: 'conversation_part',
      id: 'p2',
      part_type: 'note',
      body: 'INTERNAL: plan-gating bug from last sprint, do not tell the customer yet.',
      created_at: NOW - 6800,
      author: { type: 'admin', id: 'admin-1', name: 'Sam Support' },
      redacted: false,
    },
    {
      type: 'conversation_part',
      id: 'p3',
      part_type: 'comment',
      body: 'Chrome. Also happens in Safari.',
      created_at: NOW - 6600,
      author: {
        type: 'user',
        id: 'contact-alice',
        name: 'Alice Zhang',
        email: 'alice@customer.example',
      },
      redacted: false,
    },
  ],
  9002: [
    {
      type: 'conversation_part',
      id: 'p10',
      part_type: 'comment',
      body: 'I can pass this to the product team for you.',
      created_at: NOW - 5000,
      author: { type: 'admin', id: 'admin-fin', name: 'Fin', from_ai_agent: true },
      redacted: false,
    },
  ],
  9003: [],
}

const contacts = {
  'contact-alice': {
    type: 'contact',
    id: 'contact-alice',
    external_id: 'cust-70',
    role: 'user',
    email: 'alice@customer.example',
    name: 'Alice Zhang',
  },
  'contact-bob': {
    type: 'contact',
    id: 'contact-bob',
    external_id: '',
    role: 'lead',
    email: 'bob@lead.example',
    name: 'Bob Lee',
  },
  'contact-spam': {
    type: 'contact',
    id: 'contact-spam',
    external_id: '',
    role: 'lead',
    email: 'spam@example.com',
    name: 'Spammy',
  },
}

let requestCount = 0

function readBody(req) {
  return new Promise((resolve) => {
    let data = ''
    req.on('data', (chunk) => {
      data += chunk
    })
    req.on('end', () => resolve(data))
  })
}

function send(res, status, body) {
  requestCount += 1
  res.writeHead(status, {
    'Content-Type': 'application/json',
    'X-RateLimit-Limit': '10000',
    'X-RateLimit-Remaining': String(10000 - (requestCount % 500)),
    'X-RateLimit-Reset': String(Math.floor(Date.now() / 1000) + 10),
  })
  res.end(JSON.stringify(body))
}

function unauthorized(res) {
  send(res, 401, {
    type: 'error.list',
    request_id: 'stub-401',
    errors: [{ code: 'unauthorized', message: 'Access Token Invalid' }],
  })
}

const server = createServer(async (req, res) => {
  const url = new URL(req.url, `http://127.0.0.1:${port}`)
  const auth = req.headers.authorization ?? ''
  console.log(`[intercom-stub] ${req.method} ${url.pathname}${url.search}`)

  if (auth !== 'Bearer stub-token') {
    unauthorized(res)
    return
  }

  if (req.method === 'GET' && url.pathname === '/me') {
    send(res, 200, {
      type: 'admin',
      email: 'ops@acme.example',
      app: { type: 'app', id_code: 'ws-stub-1', name: 'Acme Stub Workspace', region: 'US' },
    })
    return
  }

  if (req.method === 'POST' && url.pathname === '/conversations/search') {
    const body = JSON.parse((await readBody(req)) || '{}')
    const filters = body.query?.value ?? []
    const gt = filters.find((f) => f.field === 'updated_at' && f.operator === '>')?.value ?? 0
    const lt =
      filters.find((f) => f.field === 'updated_at' && f.operator === '<')?.value ?? Infinity
    const matched = conversations
      .filter((c) => c.updated_at > gt && c.updated_at < lt)
      .sort((a, b) => a.updated_at - b.updated_at)
    send(res, 200, {
      type: 'conversation.list',
      total_count: matched.length,
      pages: { type: 'pages', page: 1, per_page: 150, total_pages: 1 },
      conversations: matched,
    })
    return
  }

  const convMatch = url.pathname.match(/^\/conversations\/(\d+)$/)
  if (req.method === 'GET' && convMatch) {
    const conv = conversations.find((c) => c.id === convMatch[1])
    if (!conv) {
      send(res, 404, {
        type: 'error.list',
        errors: [{ code: 'not_found', message: 'Conversation not found' }],
      })
      return
    }
    send(res, 200, {
      ...conv,
      conversation_parts: {
        type: 'conversation_part.list',
        conversation_parts: partsByConversation[conv.id] ?? [],
      },
    })
    return
  }

  if (req.method === 'POST' && url.pathname === '/contacts/search') {
    const body = JSON.parse((await readBody(req)) || '{}')
    const ids = body.query?.value?.[0]?.value ?? []
    send(res, 200, {
      type: 'list',
      data: ids.map((id) => contacts[id]).filter(Boolean),
    })
    return
  }

  send(res, 404, { type: 'error.list', errors: [{ code: 'not_found', message: 'No route' }] })
})

server.listen(port, '127.0.0.1', () => {
  console.log(`[intercom-stub] listening on http://127.0.0.1:${port}`)
  console.log('[intercom-stub] valid token: stub-token')
})
