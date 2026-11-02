// Untracked preview helper: slow fake reference search for ref-count-preview.html.
const wait = (ms: number, signal?: AbortSignal) =>
  new Promise<void>((resolve, reject) => {
    const t = setTimeout(resolve, ms)
    signal?.addEventListener('abort', () => { clearTimeout(t); reject(new DOMException('aborted', 'AbortError')) })
  })

export const api = {
  async searchSoloReferences(_id: string, p: any, signal?: AbortSignal) {
    const q = String(p.q || '').toLowerCase()
    if (p.lane === 'solo') {
      await wait(700, signal)
      const n = q ? Math.max(0, 25 - q.length * 6) : 25
      const page = p.page || 1
      const items = Array.from({ length: Math.min(20, n - (page - 1) * 20) }, (_, i) => ({
        kind: 'solo', target: `s${page}-${i}`, title: `会话 ${q} ${(page - 1) * 20 + i}`, status: 'running', lastActiveAt: '2026-11-02T10:00:00Z',
      }))
      return { lane: 'solo', items, nextPage: n > page * 20 ? page + 1 : undefined }
    }
    await wait(1500, signal)
    if (q.includes('err')) return { lane: 'github', items: [], error: { code: 'rate_limited', message: 'GitHub 限流' } }
    const big = q.includes('big')
    const counts = p.state === 'all' ? { issue: big ? 1234 : 16, pr: big ? 100 : 15 } : { issue: big ? 120 : 6, pr: big ? 99 : 0 }
    const kinds = p.kind ? [p.kind] : ['github_issue', 'github_pr']
    const items = kinds.flatMap((kind) => Array.from({ length: 3 }, (_, i) => {
      const number = (kind === 'github_pr' ? 100 : 1) + i
      return { kind, target: `acme/web#${number}`, title: `${kind === 'github_pr' ? 'PR' : 'Issue'} ${q} ${number}`, state: 'open', url: '#', comments: 1, repo: 'acme/web', number }
    }))
    return { lane: 'github', items, counts }
  },
}
