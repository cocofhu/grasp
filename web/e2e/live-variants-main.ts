const key = new URLSearchParams(location.search).get('key') || 'default'
const source = document.getElementById('live-source')!
let renderedSource = ''
let polling = false

async function refreshSource() {
  if (polling) return
  polling = true
  try {
    const state = await fetch('/__e2e/live/state?key=' + encodeURIComponent(key)).then((response) => response.json())
    if (state.source !== renderedSource) {
      renderedSource = state.source
      // The stand-in development server sends changed source to the preview.
      // The real overlay observes the resulting DOM mutation, as with HMR.
      source.innerHTML = renderedSource
    }
  } finally {
    polling = false
  }
}

async function boot() {
  await refreshSource()
  if (!localStorage.getItem('__grasp_embed')) {
    history.replaceState(null, '', location.pathname + location.search + `#__grasp_embed&run=live-e2e-${key}&node=preview&ticket=live-e2e-${key}`)
  }
  const script = document.createElement('script')
  script.src = '/preview-pick.js'
  document.head.appendChild(script)
  const timer = setInterval(() => void refreshSource(), 60)
  window.addEventListener('pagehide', () => clearInterval(timer), { once: true })
}
void boot()
