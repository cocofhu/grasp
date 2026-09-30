import { createOverlay, type HostOpts, type LiveOverlay } from './overlay'

declare global {
  interface Window {
    __graspLiveOverlay?: { version: 1; create: (opts: HostOpts) => LiveOverlay }
  }
}

// preview-pick.js loads this bundle once the drawer reports Live is on for the
// node, then calls create() with its drawer channel.
window.__graspLiveOverlay = {
  version: 1,
  create(opts: HostOpts): LiveOverlay {
    const overlay = createOverlay(opts)
    overlay.setEnabled(true)
    return overlay
  },
}
