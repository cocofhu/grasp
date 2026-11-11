import definitions from './definitions.json'

interface IconDefinition {
  body: string
  duration: number
  viewBox: string
  filled: boolean
  strokeWidth: number
  note: string
}

/** Trusted, local SVG markup. Never accept body markup from user or API input. */
export const iconDefinitions = definitions satisfies Record<string, IconDefinition>
export type IconName = keyof typeof iconDefinitions
export const iconNames = Object.keys(iconDefinitions) as IconName[]

function isIconName(name: string): name is IconName {
  return Object.prototype.hasOwnProperty.call(iconDefinitions, name)
}

const warned = new Set<string>()
export function resolveIcon(name: string): IconDefinition {
  if (isIconName(name)) return iconDefinitions[name]
  if (import.meta.env.DEV && import.meta.env.MODE !== 'test' && !warned.has(name)) {
    warned.add(name)
    console.warn(`[Icon] Unknown icon "${name}"; displaying the safe dot fallback.`)
  }
  return iconDefinitions.dot
}
