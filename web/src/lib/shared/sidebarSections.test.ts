// @vitest-environment happy-dom
import { beforeEach, describe, expect, it } from 'vitest'
import {
  STORAGE_KEY,
  __resetSidebarSectionsForTests,
  collapsedSections,
  isSectionCollapsed,
  parseStoredSections,
  toggleSection,
} from './sidebarSections'

describe('parseStoredSections', () => {
  it('keeps known sections and drops names this build does not know', () => {
    expect(parseStoredSections(['running', 'nope', 'auto', 'queued', 'history', 1, null])).toEqual([
      'running',
      'auto',
      'queued',
      'history',
    ])
    expect(parseStoredSections('running')).toEqual([])
    expect(parseStoredSections(null)).toEqual([])
  })
})

describe('collapsed sidebar sections', () => {
  beforeEach(() => {
    localStorage.clear()
    __resetSidebarSectionsForTests()
  })

  it('remembers 自动过程 apart from the other two sections', () => {
    toggleSection('auto')
    expect(isSectionCollapsed('auto')).toBe(true)
    expect(isSectionCollapsed('running')).toBe(false)
    expect(isSectionCollapsed('history')).toBe(false)
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]')).toEqual(['auto'])
    toggleSection('running')
    expect(collapsedSections.value).toEqual(['auto', 'running'])
    toggleSection('auto')
    expect(isSectionCollapsed('auto')).toBe(false)
  })

  it('remembers 排队中 without touching the other sections', () => {
    toggleSection('queued')
    expect(isSectionCollapsed('queued')).toBe(true)
    expect(isSectionCollapsed('running')).toBe(false)
    expect(isSectionCollapsed('auto')).toBe(false)
    expect(isSectionCollapsed('history')).toBe(false)
    expect(JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]')).toEqual(['queued'])
    toggleSection('queued')
    expect(isSectionCollapsed('queued')).toBe(false)
  })
})
