// Vitest 4: vi.spyOn() requires an existing function. happy-dom's Window
// does not implement window.confirm, so provide a no-op default the tests can spy on.
if (typeof window !== 'undefined' && typeof window.confirm !== 'function') {
  window.confirm = () => true
}
