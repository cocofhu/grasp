/**
 * Built-in linear identity icons. Catalog ids must stay aligned with
 * server/internal/services/agent_icon.go. This is not the UI-action catalog
 * and it does not accept caller-supplied SVG or emoji.
 */

export type IdentityColorId =
  | 'black'
  | 'red'
  | 'orange'
  | 'yellow'
  | 'green'
  | 'blue'
  | 'purple'
  | 'pink'

export interface IdentityColor {
  id: IdentityColorId
  hex: string
}

export interface IdentityIconSpec {
  id: string
  zh: string
  aliases: string[]
  body: string
}

export const IDENTITY_COLORS: IdentityColor[] = [
  { id: 'black', hex: '#18181b' },
  { id: 'red', hex: '#ef4444' },
  { id: 'orange', hex: '#f97316' },
  { id: 'yellow', hex: '#eab308' },
  { id: 'green', hex: '#22c55e' },
  { id: 'blue', hex: '#3b82f6' },
  { id: 'purple', hex: '#8b5cf6' },
  { id: 'pink', hex: '#ec4899' },
]

const DEFAULT_COLOR: IdentityColor = IDENTITY_COLORS[0]!

export const IDENTITY_ICONS: IdentityIconSpec[] = [
  { id: 'folder', zh: '文件夹', aliases: ['folder'], body: '<path d="M4 7.2A1.8 1.8 0 0 1 5.8 5.4h3.7l1.7 1.8h7a1.8 1.8 0 0 1 1.8 1.8v8.2a1.8 1.8 0 0 1-1.8 1.8H5.8A1.8 1.8 0 0 1 4 17.2z"/>' },
  { id: 'dollar', zh: '金钱', aliases: ['dollar'], body: '<path d="M12 3v18"/><path d="M16.2 7.4c-.5-1.7-2-2.6-4.2-2.6-2.5 0-4 1.2-4 2.9s1.6 2.5 4 3.1 4.4.8 4.4 2.8-1.7 3.1-4.4 3.1-4.1-1-4.6-2.7"/>' },
  { id: 'book', zh: '书本', aliases: ['book'], body: '<path d="M5 5.2A1.8 1.8 0 0 1 6.8 3.4H19v15.4H6.8A1.8 1.8 0 0 0 5 20.6z"/><path d="M6.8 3.4v15.4"/>' },
  { id: 'graduation', zh: '学士帽', aliases: ['graduation'], body: '<path d="M3 10 12 5.6 21 10 12 14.4z"/><path d="M7 11.6v3.6c1.5 1.5 8.5 1.5 10 0v-3.6"/><path d="M21 10v5.2"/>' },
  { id: 'pencil', zh: '铅笔', aliases: ['pencil'], body: '<path d="M12.8 20.2H20"/><path d="m14.2 5.2 4.6 4.6L8.2 20.4 3.4 21l.6-4.8z"/>' },
  { id: 'pen', zh: '钢笔', aliases: ['pen'], body: '<path d="M12.5 20.2H20"/><path d="M14.2 5.4 18.6 9.8"/><path d="m4.2 19.8 1-3.8L15.6 5.6a1.7 1.7 0 0 1 2.4 2.4L7.6 18.4z"/>' },
  { id: 'code', zh: '代码', aliases: ['code'], body: '<path d="m8 8-3.6 4L8 16"/><path d="m16 8 3.6 4L16 16"/><path d="m13.2 5.5-2.4 13"/>' },
  { id: 'play', zh: '播放', aliases: ['play'], body: '<path d="M8 5.6v12.8L19 12z"/>' },
  { id: 'music', zh: '音乐', aliases: ['music'], body: '<path d="M9 18.2V6.4l10-2v11.2"/><circle cx="6.2" cy="18.2" r="2.6"/><circle cx="16.2" cy="15.6" r="2.6"/>' },
  { id: 'popcorn', zh: '爆米花', aliases: ['popcorn'], body: '<path d="M7.2 9.6c.2-2 1.6-3 2.8-2.2.6-1.6 2-2 2-2s1.4.4 2 2c1.2-.8 2.6.2 2.8 2.2"/><path d="M6.6 10.2h10.8l-.9 9.2H7.5z"/><path d="M9.2 10.4v8.6M12 10.2v9M14.8 10.4v8.6"/>' },
  { id: 'palette', zh: '调色盘', aliases: ['palette'], body: '<path d="M12 4.2a7.6 7.6 0 1 0 0 15.2h1.1a1.7 1.7 0 0 0 0-3.4h-.5A1.7 1.7 0 0 1 10.9 14 4.2 4.2 0 0 1 15 9.8 3.8 3.8 0 0 0 12 4.2z"/><circle fill="currentColor" stroke="none" cx="8.3" cy="10.2" r=".8"/><circle fill="currentColor" stroke="none" cx="10.2" cy="7.6" r=".8"/><circle fill="currentColor" stroke="none" cx="13.6" cy="7.4" r=".8"/>' },
  { id: 'stethoscope', zh: '听诊器', aliases: ['stethoscope'], body: '<path d="M6.2 4v5.2a3 3 0 0 0 6 0V4"/><path d="M6.2 4H4.8M12.2 4h1.4"/><path d="M9.2 12.2V14a4.4 4.4 0 0 0 4.4 4.4h.4a2.6 2.6 0 0 0 2.6-2.6v-.6"/><circle cx="17.6" cy="12.2" r="1.8"/>' },
  { id: 'asterisk', zh: '星标', aliases: ['asterisk'], body: '<path d="M12 4.2v15.6"/><path d="M5.2 7.6 18.8 16.4"/><path d="M18.8 7.6 5.2 16.4"/>' },
  { id: 'lotus', zh: '莲花', aliases: ['lotus'], body: '<path d="M12 19.6c-3.6-2.6-5.4-5.6-5.2-8.8 2 .2 3.6 1.5 5.2 3.6 1.6-2.1 3.2-3.4 5.2-3.6.2 3.2-1.6 6.2-5.2 8.8z"/><path d="M12 14.2C10.4 11.4 10.2 8.8 11.6 6.6"/><path d="M12 14.2c1.6-2.8 1.8-5.4.4-7.6"/>' },
  { id: 'briefcase', zh: '公文包', aliases: ['briefcase'], body: '<rect x="3.5" y="8" width="17" height="11" rx="2"/><path d="M8.5 8V6.4A1.4 1.4 0 0 1 9.9 5h4.2a1.4 1.4 0 0 1 1.4 1.4V8"/><path d="M3.5 13h17"/>' },
  { id: 'chart', zh: '图表', aliases: ['chart'], body: '<path d="M4 19.2h16"/><path d="M7.2 16.2V10"/><path d="M12 16.2V6.2"/><path d="M16.8 16.2v-4.2"/>' },
  { id: 'kettlebell', zh: '壶铃', aliases: ['kettlebell'], body: '<path d="M9 9.2a3 3 0 0 1 6 0"/><path d="M7.4 9.4h9.2v1.6a5.2 5.2 0 0 1-4.6 5.2 5.2 5.2 0 0 1-4.6-5.2z"/>' },
  { id: 'dumbbell', zh: '哑铃', aliases: ['dumbbell'], body: '<path d="M6.2 9.2v5.6M17.8 9.2v5.6M8.4 8.2v7.6M15.6 8.2v7.6M8.4 12h7.2M4.2 10.6v2.8M19.8 10.6v2.8"/>' },
  { id: 'calendar', zh: '日历', aliases: ['calendar'], body: '<rect x="4" y="5.2" width="16" height="14.2" rx="2"/><path d="M4 9.6h16M8 3.6v3.2M16 3.6v3.2"/>' },
  { id: 'scales', zh: '天平', aliases: ['scales'], body: '<path d="M12 4.2v14.6"/><path d="M7 19.6h10"/><path d="M5 9.2h14"/><path d="m8.2 9.2-2.2 5.2h4.4z"/><path d="m15.8 9.2-2.2 5.2h4.4z"/>' },
  { id: 'compass', zh: '罗盘', aliases: ['compass'], body: '<circle cx="12" cy="12" r="8"/><path d="m15.4 8.6-1.8 5-5 1.8 1.8-5z"/>' },
  { id: 'plane', zh: '飞机', aliases: ['plane'], body: '<path d="M21 4.6 10.2 12.2 8.4 18l2.2-4.6L21 4.6z"/><path d="M10.2 12.2 4.6 10.4l4.2-1.6"/>' },
  { id: 'globe', zh: '地球', aliases: ['globe'], body: '<circle cx="12" cy="12" r="8"/><path d="M4 12h16"/><path d="M12 4c2.1 2.3 3.2 5 3.2 8s-1.1 5.7-3.2 8c-2.1-2.3-3.2-5-3.2-8s1.1-5.7 3.2-8z"/>' },
  { id: 'wrench', zh: '扳手', aliases: ['wrench'], body: '<path d="M14.6 6.4a3.6 3.6 0 0 0-4.8 4.8L4.6 16.4l3 3 5.2-5.2a3.6 3.6 0 0 0 4.8-4.8L15 12l-3-3z"/>' },
  { id: 'paw', zh: '爪印', aliases: ['paw'], body: '<circle cx="7" cy="8.2" r="1.45"/><circle cx="10.3" cy="5.8" r="1.45"/><circle cx="13.7" cy="5.8" r="1.45"/><circle cx="17" cy="8.2" r="1.45"/><path d="M8.2 14.4c.5-1.5 1.6-2.2 3.8-2.2s3.3.7 3.8 2.2c.7 2 .2 4.2-1.4 5.1-.9.5-1.6.1-2.1-.6-.4-.5-.7-.7-1.5-.7s-1.1.2-1.5.7c-.5.7-1.2 1.1-2.1.6-1.6-.9-2.1-3.1-1.4-5.1z"/>' },
  { id: 'flask', zh: '烧瓶', aliases: ['flask'], body: '<path d="M9 3.4h6"/><path d="M10 3.4v5.6L5.6 19a1.8 1.8 0 0 0 1.6 2.6h9.6a1.8 1.8 0 0 0 1.6-2.6L14 9V3.4"/><path d="M8.2 14.2h7.6"/>' },
  { id: 'brain', zh: '大脑', aliases: ['brain'], body: '<path d="M9.2 8.2A2.6 2.6 0 0 1 12 5.6a2.6 2.6 0 0 1 2.8 2.6 2.5 2.5 0 0 1 2 2.4c0 2.6-1.6 5-4.8 6-3.2-1-4.8-3.4-4.8-6a2.5 2.5 0 0 1 2-2.4z"/><path d="M12 8.2v8.4"/><path d="M9.4 11.2c.8.4 1.6.5 2.6.5M14.6 12.6c-.8.3-1.6.4-2.6.4"/>' },
  { id: 'heart', zh: '心', aliases: ['heart'], body: '<path d="M12 19.6 5.2 12.6a4 4 0 0 1 5.6-5.7L12 8.2l1.2-1.3a4 4 0 0 1 5.6 5.7z"/>' },
  { id: 'plant', zh: '植物', aliases: ['plant'], body: '<path d="M12 20.4V11.2"/><path d="M12 13.2c0-3.6 3.2-6 6.4-5.1-1 3.6-3.2 5.1-6.4 5.1z"/><path d="M12 11.6C12 8 8.8 5.8 5.6 6.6c1 3.4 3.2 5 6.4 5z"/>' },
]

const iconById = new Map(IDENTITY_ICONS.map((icon) => [icon.id, icon]))
const colorById = new Map(IDENTITY_COLORS.map((color) => [color.id, color]))

export function identityIcon(id: string | undefined | null): IdentityIconSpec | undefined {
  if (!id) return undefined
  return iconById.get(id)
}

export function identityColor(id: string | undefined | null): IdentityColor | undefined {
  if (!id) return undefined
  return colorById.get(id as IdentityColorId)
}

export function isIdentityColor(id: string | undefined | null): id is IdentityColorId {
  return !!id && colorById.has(id as IdentityColorId)
}

/** Saved color, otherwise black. Unknown ids stay unset so callers can fall back. */
export function identityColorOrDefault(id: string | undefined | null): IdentityColor {
  return identityColor(id) ?? DEFAULT_COLOR
}

export function searchIdentityIcons(query: string): IdentityIconSpec[] {
  const q = query.trim().toLowerCase()
  if (!q) return IDENTITY_ICONS
  return IDENTITY_ICONS.filter((icon) => {
    if (icon.zh.toLowerCase().includes(q)) return true
    return icon.aliases.some((alias) => alias.toLowerCase().includes(q))
  })
}

export function identityIconLabel(icon: IdentityIconSpec, locale: string): string {
  if (/^zh/i.test(locale)) return icon.zh
  return icon.aliases[0] || icon.zh
}

/** Slot ink plus a ~16% wash of the same color. */
export function identityTint(colorId: string | undefined | null): { color: string; background: string } | null {
  const color = identityColor(colorId)
  if (!color) return null
  return {
    color: color.hex,
    background: `color-mix(in srgb, ${color.hex} 16%, transparent)`,
  }
}
