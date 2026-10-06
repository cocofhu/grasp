export type ProjectTreeChild = {
  id: string
  label: string
  count?: number
  icon?: string
}

export type ProjectTreeNode = {
  id: string
  label: string
  count?: number
  icon?: string
  children?: ProjectTreeChild[]
}

export function projectKey(id: string): string {
  return 'p:' + id
}

export function childKey(pid: string, cid: string): string {
  return 'c:' + pid + ':' + cid
}

/** Project ids must not contain ':'; child ids may. */
export function parseTreeKey(key: string): { projectId: string; childId?: string } | null {
  if (key.startsWith('p:')) {
    const projectId = key.slice(2)
    return projectId ? { projectId } : null
  }
  if (key.startsWith('c:')) {
    const rest = key.slice(2)
    const sep = rest.indexOf(':')
    if (sep <= 0 || sep === rest.length - 1) return null
    return { projectId: rest.slice(0, sep), childId: rest.slice(sep + 1) }
  }
  return null
}

export type ProjectTreeRow =
  | { kind: 'project'; key: string; node: ProjectTreeNode; expanded: boolean; hasChildren: boolean }
  | { kind: 'child'; key: string; projectId: string; child: ProjectTreeChild }

export type FilteredTree = {
  nodes: ProjectTreeNode[]
  /** Projects forced open because a child label matched the query. */
  forcedOpen: Set<string>
}

/**
 * A project matching the query keeps all its children; otherwise only matching
 * children are kept and the project is forced open.
 */
export function filterProjectTree(nodes: ProjectTreeNode[], query: string): FilteredTree {
  const q = query.trim().toLowerCase()
  const forcedOpen = new Set<string>()
  if (!q) return { nodes, forcedOpen }
  const out: ProjectTreeNode[] = []
  for (const node of nodes) {
    if (node.label.toLowerCase().includes(q)) {
      out.push(node)
      continue
    }
    const children = (node.children ?? []).filter((c) => c.label.toLowerCase().includes(q))
    if (children.length) {
      out.push({ ...node, children })
      forcedOpen.add(node.id)
    }
  }
  return { nodes: out, forcedOpen }
}

export function flattenProjectTree(
  nodes: ProjectTreeNode[],
  isExpanded: (projectId: string) => boolean,
): ProjectTreeRow[] {
  const rows: ProjectTreeRow[] = []
  for (const node of nodes) {
    const hasChildren = (node.children?.length ?? 0) > 0
    const expanded = hasChildren && isExpanded(node.id)
    rows.push({ kind: 'project', key: projectKey(node.id), node, expanded, hasChildren })
    if (!expanded) continue
    for (const child of node.children ?? []) {
      rows.push({ kind: 'child', key: childKey(node.id, child.id), projectId: node.id, child })
    }
  }
  return rows
}

export function loadExpandedProjects(storageKey: string): Set<string> | null {
  try {
    const raw = localStorage.getItem(storageKey)
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    if (!Array.isArray(parsed)) return null
    return new Set(parsed.filter((v): v is string => typeof v === 'string'))
  } catch {
    return null
  }
}

export function saveExpandedProjects(storageKey: string, expanded: Set<string>): void {
  try {
    localStorage.setItem(storageKey, JSON.stringify([...expanded]))
  } catch {
    /* storage unavailable (private mode / quota) */
  }
}
