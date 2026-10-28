import type { Agent, ModelBinding, SharedAgentConfig } from '@/lib/api/apiTypes'

/** Only selectors project inheritance; editing keeps the Agent's local values. */
export function effectiveModelAgent(agent: Agent, defaults?: SharedAgentConfig): Agent {
  return agent.inheritModel ? { ...agent, modelBinding: defaults?.modelBinding, acpBackend: defaults?.acpBackend || agent.acpBackend } : agent
}

/** The default is first in selectors; stored additional-group order is stable. */
export function boundConfigurationIds(binding?: ModelBinding | null): string[] {
  if (!binding?.configurationId) return []
  return [...new Set([binding.configurationId, ...(binding.configurationIds || [])].filter(Boolean))]
}

const routePrefix = '@group:'
export function modelRouteValue(configurationId: string, modelId: string): string {
  return routePrefix + JSON.stringify([configurationId, modelId])
}
export function parseModelRoute(value: string): { configurationId?: string; modelId: string } {
  if (value.startsWith(routePrefix)) {
    try {
      const route: unknown = JSON.parse(value.slice(routePrefix.length))
      if (Array.isArray(route) && route.length === 2 && route.every(item => typeof item === 'string')) return { configurationId: route[0], modelId: route[1] }
    } catch { /* provider model IDs are otherwise opaque */ }
  }
  return { modelId: value }
}
