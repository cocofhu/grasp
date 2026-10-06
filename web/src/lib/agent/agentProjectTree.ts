import type { ProjectTreeNode } from '@/components/ui/projectTree'

export type AgentProjectRef = { name: string; projectId: string }
export type ProjectRef = { id: string; name: string }

/** Project → Agent nodes for ProjectTree; Agents whose project is not listed are omitted. */
export function toProjectTreeNodes(agents: AgentProjectRef[], projects: ProjectRef[]): ProjectTreeNode[] {
  const byProject = new Map<string, string[]>()
  for (const a of agents) {
    const list = byProject.get(a.projectId)
    if (list) list.push(a.name)
    else byProject.set(a.projectId, [a.name])
  }
  return projects.map((p) => {
    const names = [...(byProject.get(p.id) || [])].sort((a, b) => a.localeCompare(b))
    return {
      id: p.id,
      label: p.name,
      count: names.length,
      children: names.map((name) => ({ id: name, label: name, icon: 'robot' })),
    }
  })
}
