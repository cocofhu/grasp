import { onBeforeUnmount, ref, watch, type Ref } from 'vue'

/** Set only while an AppShell is mounted; public / embedded hosts have no drawer to open. */
let opener: (() => void) | null = null
export const mobileNavAvailable = ref(false)

/** Headers currently rendering their own nav button; the floating ball yields to them. */
export const inlineMobileNavHosts = ref(0)

export function provideMobileNavOpener(open: () => void) {
  opener = open
  mobileNavAvailable.value = true
  onBeforeUnmount(() => {
    if (opener !== open) return
    opener = null
    mobileNavAvailable.value = false
  })
}

export function openMobileNav() {
  opener?.()
}

/** Register a header nav button while `active` is true. */
export function useInlineMobileNav(active: Ref<boolean>) {
  let held = false
  const sync = (next: boolean) => {
    if (next === held) return
    held = next
    inlineMobileNavHosts.value += next ? 1 : -1
  }
  watch(active, sync, { immediate: true })
  onBeforeUnmount(() => sync(false))
}
