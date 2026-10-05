import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const css = readFileSync(join(dirname(fileURLToPath(import.meta.url)), '../../styles/global.css'), 'utf8')

describe('global loading reduced-motion (g4.4)', () => {
  it('disables spin, pulse, shimmer, refresh bar, drawer and toast motion', () => {
    expect(css).toMatch(/prefers-reduced-motion:\s*reduce/)
    expect(css).toMatch(/\.app-spinner/)
    expect(css).toMatch(/\.app-skeleton__block::after/)
    expect(css).toMatch(/\.app-refresh-bar/)
    expect(css).toMatch(/\.animate-spin/)
    expect(css).toMatch(/\.animate-pulse/)
    expect(css).toMatch(/\.drawer-fade-enter-active/)
    expect(css).toMatch(/\.toast-enter-active/)
    expect(css).toMatch(/\.home-to-gates-enter-active/)
    expect(css).toMatch(/width:\s*100%/)
    expect(css).toMatch(/\.app-desktop-sidebar/)
    expect(css).toMatch(/transition:\s*none/)
  })

  it('defines motion tokens and gates new Transition classes (g1.1 / g1.2 / g3.2)', () => {
    expect(css).toMatch(/--dur-press:\s*90ms/)
    expect(css).toMatch(/--dur-ui:\s*160ms/)
    expect(css).toMatch(/--dur-overlay:\s*200ms/)
    expect(css).toMatch(/--ease-out-expo:\s*cubic-bezier\(0\.16,\s*1,\s*0\.3,\s*1\)/)
    expect(css).toMatch(/\.ui-fade-enter-active/)
    expect(css).toMatch(/\.overlay-pop-enter-active/)
    expect(css).toMatch(/\.ui-pressable/)
    expect(css).toMatch(/\.list-card-lift/)
    expect(css).toMatch(/\.ui-tip-fade-enter-active/)
    expect(css).toMatch(/\.ui-fold-chevron/)
    expect(css).toMatch(/\.ui-fold\b/)
    expect(css).toMatch(/\.ui-tip-fade-enter-active[\s\S]*transition:\s*none/)
    expect(css).toMatch(/\.ui-fold-chevron[\s\S]*transition:\s*none/)
  })

  it('source-contracts: filters / board card / lang select use overlay-pop or list-card-lift (g1.1 / g2.1)', () => {
    const root = join(dirname(fileURLToPath(import.meta.url)), '../..')
    const read = (rel: string) => readFileSync(join(root, rel), 'utf8')
    expect(read('components/ui/ProjectFilter.vue')).toMatch(/name="overlay-pop"/)
    expect(read('components/ui/StatusFilter.vue')).toMatch(/name="overlay-pop"/)
    expect(read('components/ui/WorkflowFilter.vue')).toMatch(/name="overlay-pop"/)
    expect(read('components/ui/LangSelect.vue')).toMatch(/name="overlay-pop"/)
    expect(read('components/board/RunBoardCard.vue')).toMatch(/list-card-lift/)
    expect(read('components/board/RunBoardCard.vue')).not.toMatch(/translateY\(-1px\)/)
    expect(read('components/ui/AppModal.vue')).toMatch(/var\(--dur-overlay\)/)
    expect(read('components/ui/AppDrawer.vue')).toMatch(/var\(--dur-overlay\)/)
    expect(read('components/ui/ToastHost.vue')).toMatch(/var\(--dur-overlay\)/)
    expect(read('components/shell/AppShell.vue')).toMatch(/var\(--dur-overlay\)/)
  })
})
