import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { MCPPreset } from '@/lib/api/apiTypes'

const listMcpPresets = vi.fn()
vi.mock('@/lib/api/api', () => ({ api: { listMcpPresets: () => listMcpPresets() } }))

import { findPreset, loadMcpPresets, presetDraft, resetMcpPresetsCache } from './mcpPresets'

const desktop = {
  id: 'desktop',
  requiresDesktop: true,
  server: { name: 'desktop', command: '/opt/grasp-runtime/current/bin/grasp-desktop', args: ['mcp'] },
} as unknown as MCPPreset

describe('mcpPresets', () => {
  beforeEach(() => {
    resetMcpPresetsCache()
    listMcpPresets.mockReset()
  })

  it('fetches once and caches the presets', async () => {
    listMcpPresets.mockResolvedValue({ presets: [desktop] })
    expect(await loadMcpPresets()).toEqual([desktop])
    expect(await loadMcpPresets()).toEqual([desktop])
    expect(listMcpPresets).toHaveBeenCalledTimes(1)
  })

  it('treats a missing list as empty', async () => {
    listMcpPresets.mockResolvedValue({})
    expect(await loadMcpPresets()).toEqual([])
  })

  it('returns empty on failure and retries next time', async () => {
    listMcpPresets.mockRejectedValueOnce(new Error('down')).mockResolvedValue({ presets: [desktop] })
    expect(await loadMcpPresets()).toEqual([])
    expect(await loadMcpPresets()).toEqual([desktop])
    expect(listMcpPresets).toHaveBeenCalledTimes(2)
  })

  it('finds presets and converts them to drafts', () => {
    expect(findPreset([desktop], 'desktop')).toBe(desktop)
    expect(findPreset([desktop], 'blender')).toBeUndefined()
    const d = presetDraft(desktop)
    expect(d.name).toBe('desktop')
    expect(d.command).toBe('/opt/grasp-runtime/current/bin/grasp-desktop')
  })
})
