/** True when document uses light chrome (html.light), including public force-light. */
export function isLightTheme(root: Element = document.documentElement): boolean {
  return root.classList.contains('light')
}

/** Convert Grasp `--c-*` space-separated RGB channels into css color strings. */
export function cssTokenColor(name: string, fallback: string, root: Element = document.documentElement): string {
  const raw = getComputedStyle(root).getPropertyValue(name).trim()
  if (!raw) return fallback
  if (raw.startsWith('#') || raw.startsWith('rgb') || raw.startsWith('hsl')) return raw
  const channels = raw.split(/[\s,]+/).filter(Boolean)
  if (channels.length >= 3) return `rgb(${channels.slice(0, 3).join(', ')})`
  return fallback
}

export function themeVars(root: Element = document.documentElement): Record<string, string> {
  const light = isLightTheme(root)
  const txt = cssTokenColor('--c-txt', light ? 'rgb(24, 24, 27)' : 'rgb(237, 237, 240)', root)
  const txt2 = cssTokenColor('--c-txt2', light ? 'rgb(82, 82, 91)' : 'rgb(161, 161, 170)', root)
  const border = cssTokenColor('--c-line-strong', light ? 'rgb(208, 208, 216)' : 'rgb(54, 54, 62)', root)
  return {
    background: cssTokenColor('--c-base', light ? 'rgb(250, 250, 251)' : 'rgb(10, 10, 11)', root),
    primaryColor: cssTokenColor('--c-elevated', light ? 'rgb(244, 244, 245)' : 'rgb(28, 28, 33)', root),
    primaryTextColor: txt,
    primaryBorderColor: border,
    clusterBorder: border,
    // Edges are thin strokes; a border token disappears against the light canvas.
    lineColor: cssTokenColor('--c-txt3', light ? 'rgb(82, 82, 91)' : 'rgb(110, 110, 120)', root),
    secondaryColor: cssTokenColor('--c-elevated', light ? 'rgb(244, 244, 245)' : 'rgb(28, 28, 33)', root),
    tertiaryColor: cssTokenColor('--c-base', light ? 'rgb(250, 250, 251)' : 'rgb(10, 10, 11)', root),
    // Node / edge / secondary labels: keep readable on light chrome (avoid faint gray-on-white).
    secondaryTextColor: txt,
    tertiaryTextColor: txt2,
    textColor: txt,
    titleColor: txt,
    nodeTextColor: txt,
    clusterBkg: cssTokenColor('--c-elevated', light ? 'rgb(244, 244, 245)' : 'rgb(28, 28, 33)', root),
    clusterTextColor: txt,
    edgeLabelBackground: cssTokenColor('--c-base', light ? 'rgb(250, 250, 251)' : 'rgb(10, 10, 11)', root),
    actorTextColor: txt,
    labelTextColor: txt,
  }
}

export function mermaidThemeName(root: Element = document.documentElement): 'base' | 'dark' {
  return isLightTheme(root) ? 'base' : 'dark'
}
