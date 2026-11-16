import { api } from '@/lib/api/api'
import type { MCPPreset } from '@/lib/api/apiTypes'
import { apiMcpToDraft, type DraftMCP } from '@/lib/agent/agentStudioDraft'

export type { MCPPreset }

export const MCP_PRESET_LABEL_KEYS: Record<MCPPreset['id'], string> = {
  'artifact-store': 'pages.agentStudio.mcp.addArtifactStore',
  browser: 'pages.agentStudio.mcp.addBrowser',
  desktop: 'pages.agentStudio.mcp.addDesktop',
  blender: 'pages.agentStudio.mcp.addBlender',
}

let cached: Promise<MCPPreset[]> | null = null

/** Built-in MCP templates from the server, fetched once per page load. */
export function loadMcpPresets(): Promise<MCPPreset[]> {
  cached ??= api
    .listMcpPresets()
    .then((r) => r.presets ?? [])
    .catch(() => {
      cached = null
      return []
    })
  return cached
}

export function resetMcpPresetsCache() {
  cached = null
}

export function presetDraft(p: MCPPreset): DraftMCP {
  return apiMcpToDraft(p.server)
}

export function findPreset(presets: MCPPreset[], id: MCPPreset['id']): MCPPreset | undefined {
  return presets.find((p) => p.id === id)
}
