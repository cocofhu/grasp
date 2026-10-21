/**
 * Local visual preview only. Not part of the app: serves the real shell with
 * a same-origin API stand-in so the workflow list can be opened without a backend.
 */
const workflows = [
  {
    id: 'wf-default',
    name: '默认工作流',
    description: '套用开箱基线，填仓库即可运行',
    status: 'published',
    version: 2,
    publishedVersion: 2,
    updatedAt: '2026-10-18T08:00:00Z',
    needsRepo: true,
    showOnHome: true,
    nodes: [],
    edges: [],
  },
  {
    id: 'wf-align',
    name: '需求对齐工作流',
    description: '从澄清开始，把第一句话作为运行标题',
    status: 'draft',
    version: 1,
    publishedVersion: 0,
    updatedAt: '2026-10-12T08:00:00Z',
    needsRepo: false,
    showOnHome: false,
    nodes: [],
    edges: [],
  },
]

const page = { items: [], total: 0, page: 1, pageSize: 20, hasMore: false }

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { 'Content-Type': 'application/json', Date: new Date().toUTCString() },
  })
}

function previewResponse(path: string): Response {
  if (path.startsWith('/auth/me')) {
    return json({ username: 'preview', expires_at: '2099-01-01T00:00:00Z', is_admin: true })
  }
  if (path.startsWith('/health') || path.startsWith('/live')) return json({ status: 'ok', commit: 'preview' })
  if (path.startsWith('/workspace')) return json({ name: 'Grasp', description: '', variables: [] })
  if (path.startsWith('/workflows/catalog')) {
    return json(workflows.map(({ nodes: _nodes, edges: _edges, ...item }) => item))
  }
  if (path === '/workflows' || path.startsWith('/workflows?')) return json(workflows)
  if (path.startsWith('/runs')) return json(page)
  if (path.startsWith('/notifications')) return json({ items: [], total: 0, unreadCount: 0, allCount: 0, readCount: 0 })
  if (path.startsWith('/agents')) return json([])
  if (path.split('?')[0].endsWith('s')) return json([])
  return json({})
}

const nativeFetch = window.fetch.bind(window)
window.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
  const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
  const path = raw.replace(/^https?:\/\/[^/]+/, '')
  if (path.startsWith('/api/')) return Promise.resolve(previewResponse(path.slice(4)))
  return nativeFetch(input, init)
}

localStorage.setItem('grasp-theme', 'light')

void import('./main.ts')
