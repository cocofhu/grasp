import type { Plugin } from 'vite'

/**
 * Strip noVNC's Secure Context warning so HTTP (non-localhost) deployments work.
 * x11vnc -nopw uses None auth; crypto.subtle is not required for the preview path.
 * Localhost is already a Secure Context — this only matters for http://hostname.
 */
export const SECURE_CONTEXT_SNIPPET = 'noVNC requires a secure context (TLS)'

// The dev prebundle (esbuild) drops the comments above the check, so they are optional.
const BLOCK_RE =
  /(?:\/\/ We rely on modern APIs which might not be available in an\s*\n\s*\/\/ insecure context\s*\n\s*)?if\s*\(\s*!window\.isSecureContext\s*\)\s*\{\s*\n?\s*Log\.Error\("noVNC requires a secure context \(TLS\)\. Expect crashes!"\);\s*\n?\s*\}\s*\n?/

/** Pure transform used by the Vite plugin and unit tests. */
export function stripNovncSecureContextCheck(code: string): string | null {
  if (!code.includes(SECURE_CONTEXT_SNIPPET)) return null
  const next = code.replace(BLOCK_RE, '')
  return next === code ? null : next
}

/** Source path in builds; `.vite/deps/@novnc_novnc_lib_rfb__js.js` in the dev prebundle. */
export function isNovncRfbModule(id: string): boolean {
  return /@novnc[/_]novnc/.test(id) && /rfb(__js)?\.js/.test(id)
}

export function stripNovncSecureContext(): Plugin {
  return {
    name: 'strip-novnc-secure-context',
    transform(code, id) {
      if (!isNovncRfbModule(id)) return null
      const next = stripNovncSecureContextCheck(code)
      if (next == null) return null
      return { code: next, map: null }
    },
  }
}
