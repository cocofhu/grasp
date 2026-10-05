// @vitest-environment happy-dom
import { beforeEach, describe, expect, it } from 'vitest'
import {
  GRASP_STORAGE_KEYS,
  LEGACY_STORAGE_KEYS,
  __resetBrandStorageMigrationForTests,
  migrateLocalStorageKey,
  migrateLocalStoragePrefix,
  migrateSessionStorageKey,
  runBrandStorageMigration,
} from './migrateBrandStorage'

describe('migrateBrandStorage', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    __resetBrandStorageMigrationForTests()
  })

  it('copies old key to new and deletes old when new is empty', () => {
    localStorage.setItem(LEGACY_STORAGE_KEYS.locale, 'zh-CN')
    expect(migrateLocalStorageKey(LEGACY_STORAGE_KEYS.locale, GRASP_STORAGE_KEYS.locale)).toBe(
      'zh-CN',
    )
    expect(localStorage.getItem(GRASP_STORAGE_KEYS.locale)).toBe('zh-CN')
    expect(localStorage.getItem(LEGACY_STORAGE_KEYS.locale)).toBeNull()
  })

  it('keeps new key and deletes old when both exist', () => {
    localStorage.setItem(LEGACY_STORAGE_KEYS.locale, 'en')
    localStorage.setItem(GRASP_STORAGE_KEYS.locale, 'zh-CN')
    expect(migrateLocalStorageKey(LEGACY_STORAGE_KEYS.locale, GRASP_STORAGE_KEYS.locale)).toBe(
      'zh-CN',
    )
    expect(localStorage.getItem(GRASP_STORAGE_KEYS.locale)).toBe('zh-CN')
    expect(localStorage.getItem(LEGACY_STORAGE_KEYS.locale)).toBeNull()
  })

  it('migrates prefix keys for favorites', () => {
    localStorage.setItem('approving.workflowFavorites.alice', '[{"workflowId":"w1","favoritedAt":1}]')
    localStorage.setItem('approving.workflowFavorites.alice.order-v2', '1')
    migrateLocalStoragePrefix(
      LEGACY_STORAGE_KEYS.workflowFavoritesPrefix,
      GRASP_STORAGE_KEYS.workflowFavoritesPrefix,
    )
    expect(localStorage.getItem('grasp.workflowFavorites.alice')).toContain('w1')
    expect(localStorage.getItem('grasp.workflowFavorites.alice.order-v2')).toBe('1')
    expect(localStorage.getItem('approving.workflowFavorites.alice')).toBeNull()
  })

  it('migrates sessionStorage gate share URLs', () => {
    sessionStorage.setItem('approving.gateShareUrl.r1:n1:0', 'https://share.example/x')
    expect(
      migrateSessionStorageKey(
        'approving.gateShareUrl.r1:n1:0',
        'grasp.gateShareUrl.r1:n1:0',
      ),
    ).toBe('https://share.example/x')
    expect(sessionStorage.getItem('grasp.gateShareUrl.r1:n1:0')).toBe('https://share.example/x')
    expect(sessionStorage.getItem('approving.gateShareUrl.r1:n1:0')).toBeNull()
  })

  it('runBrandStorageMigration is idempotent and covers fixed keys', () => {
    localStorage.setItem(LEGACY_STORAGE_KEYS.theme, 'light')
    localStorage.setItem(LEGACY_STORAGE_KEYS.homeLastWorkflowId, 'wf-1')
    runBrandStorageMigration()
    runBrandStorageMigration()
    expect(localStorage.getItem(GRASP_STORAGE_KEYS.theme)).toBe('light')
    expect(localStorage.getItem(GRASP_STORAGE_KEYS.homeLastWorkflowId)).toBe('wf-1')
    expect(localStorage.getItem(LEGACY_STORAGE_KEYS.theme)).toBeNull()
  })
})
