// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest'
import { redirectToLogin } from './httpCore'

afterEach(() => {
  vi.restoreAllMocks()
  history.replaceState(null, '', '/')
})

describe('redirectToLogin', () => {
  it('sends app pages to login with the return path', () => {
    const assign = vi.spyOn(window.location, 'assign').mockImplementation(() => {})
    history.replaceState(null, '', '/runs/r1?tab=log')
    redirectToLogin()
    expect(assign).toHaveBeenCalledWith(`/login?redirect=${encodeURIComponent('/runs/r1?tab=log')}`)
  })

  it.each(['/login', '/public/gate-approvals', '/embed/runs/r1/nodes/n1/chat'])('leaves %s alone', (path) => {
    const assign = vi.spyOn(window.location, 'assign').mockImplementation(() => {})
    history.replaceState(null, '', path)
    redirectToLogin()
    expect(assign).not.toHaveBeenCalled()
  })
})
