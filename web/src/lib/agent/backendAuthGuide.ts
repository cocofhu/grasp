import type { BackendId } from '@/lib/shared/regionPolicy'

export type AuthApplyLink = {
  labelKey: string
  url: string
}

export type AuthKeySpec = {
  key: string
  alt?: string
}

export type BackendAuthGuide = {
  backend: BackendId
  keys: AuthKeySpec[]
  /** i18n keys for console path steps (ordered). */
  pathStepKeys: string[]
  /** Official docs / console entry links. */
  links: AuthApplyLink[]
  /** Optional note i18n key (e.g. site-bound keys). */
  noteKey?: string
}

const CURSOR_GUIDE: BackendAuthGuide = {
  backend: 'cursor',
  keys: [{ key: 'GRASP_CURSOR_API_KEY', alt: 'CURSOR_API_KEY' }],
  pathStepKeys: [
    'pages.agentStudio.wizard.apiKey.paths.cursor.step1',
    'pages.agentStudio.wizard.apiKey.paths.cursor.step2',
    'pages.agentStudio.wizard.apiKey.paths.cursor.step3',
  ],
  links: [
    { labelKey: 'pages.agentStudio.wizard.apiKey.links.cursorDashboard', url: 'https://cursor.com/dashboard' },
    { labelKey: 'pages.agentStudio.wizard.apiKey.links.cursorDocs', url: 'https://cursor.com/docs' },
  ],
}

const CLAUDE_GUIDE: BackendAuthGuide = {
  backend: 'claude_code',
  keys: [{ key: 'GRASP_CLAUDE_API_KEY', alt: 'ANTHROPIC_API_KEY' }],
  pathStepKeys: [
    'pages.agentStudio.wizard.apiKey.paths.claude.step1',
    'pages.agentStudio.wizard.apiKey.paths.claude.step2',
    'pages.agentStudio.wizard.apiKey.paths.claude.step3',
  ],
  links: [
    {
      labelKey: 'pages.agentStudio.wizard.apiKey.links.claudeConsole',
      url: 'https://console.anthropic.com/',
    },
    {
      labelKey: 'pages.agentStudio.wizard.apiKey.links.claudeKeys',
      url: 'https://platform.claude.com/settings/keys',
    },
    {
      labelKey: 'pages.agentStudio.wizard.apiKey.links.claudeDocs',
      url: 'https://docs.anthropic.com/',
    },
  ],
}

const CODEBUDDY_BASE: Omit<BackendAuthGuide, 'links'> = {
  backend: 'codebuddy',
  keys: [{ key: 'GRASP_CODEBUDDY_API_KEY', alt: 'CODEBUDDY_API_KEY' }],
  pathStepKeys: [
    'pages.agentStudio.wizard.apiKey.paths.codebuddy.step1',
    'pages.agentStudio.wizard.apiKey.paths.codebuddy.step2',
    'pages.agentStudio.wizard.apiKey.paths.codebuddy.step3',
  ],
  noteKey: 'pages.agentStudio.wizard.apiKey.notes.codebuddySite',
}

const OPENCODE_GUIDE: BackendAuthGuide = {
  backend: 'opencode',
  keys: [{ key: 'GRASP_OPENCODE_API_KEY', alt: 'OPENCODE_API_KEY' }],
  pathStepKeys: [
    'pages.agentStudio.wizard.apiKey.paths.opencode.step1',
    'pages.agentStudio.wizard.apiKey.paths.opencode.step2',
    'pages.agentStudio.wizard.apiKey.paths.opencode.step3',
  ],
  links: [
    {
      labelKey: 'pages.agentStudio.wizard.apiKey.links.opencodeProviders',
      url: 'https://opencode.ai/docs/providers/',
    },
  ],
  noteKey: 'pages.agentStudio.wizard.apiKey.notes.opencode',
}

const TRAE_GUIDE: BackendAuthGuide = {
  backend: 'trae',
  keys: [
    { key: 'GRASP_TRAE_API_KEY', alt: 'TRAECLI_PERSONAL_ACCESS_TOKEN' },
  ],
  pathStepKeys: [
    'pages.agentStudio.wizard.apiKey.paths.trae.step1',
    'pages.agentStudio.wizard.apiKey.paths.trae.step2',
    'pages.agentStudio.wizard.apiKey.paths.trae.step3',
  ],
  links: [
    {
      labelKey: 'pages.agentStudio.wizard.apiKey.links.traeTokenDocs',
      url: 'https://docs.trae.cn/cli_login-token',
    },
  ],
  noteKey: 'pages.agentStudio.wizard.apiKey.notes.traeToken',
}

const CODEBUDDY_SITE_LINKS: Record<string, AuthApplyLink> = {
  public: {
    labelKey: 'pages.agentStudio.wizard.apiKey.links.codebuddyPublic',
    url: 'https://www.codebuddy.ai/profile/keys',
  },
  internal: {
    labelKey: 'pages.agentStudio.wizard.apiKey.links.codebuddyInternal',
    url: 'https://copilot.tencent.com/profile/',
  },
  ioa: {
    labelKey: 'pages.agentStudio.wizard.apiKey.links.codebuddyIoa',
    url: 'https://tencent.sso.copilot.tencent.com/profile/keys',
  },
  staging: {
    labelKey: 'pages.agentStudio.wizard.apiKey.links.codebuddyStaging',
    url: 'https://staging-codebuddy.tencent.com/profile/keys',
  },
}

/** Studio Env badge text (short). Kept for reuse with wizard auth step. */
export const BACKEND_AUTH_HINTS: Record<
  BackendId,
  { key: string; alt?: string; note: string }
> = {
  codex: { key: 'OPENAI_API_KEY', alt: 'CODEX_API_KEY', note: '可留空：复用 Grasp 服务端配置的本机 Codex 登录（GRASP_CODEX_AUTH_FILE）' },
  cursor: { key: 'GRASP_CURSOR_API_KEY', alt: 'CURSOR_API_KEY', note: 'Cursor ACP 鉴权' },
  claude_code: {
    key: 'GRASP_CLAUDE_API_KEY',
    alt: 'ANTHROPIC_API_KEY',
    note: 'Claude Code ACP 鉴权',
  },
  codebuddy: {
    key: 'GRASP_CODEBUDDY_API_KEY',
    alt: 'CODEBUDDY_API_KEY',
    note: 'CodeBuddy ACP 鉴权',
  },
  trae: {
    key: 'GRASP_TRAE_API_KEY',
    alt: 'TRAECLI_PERSONAL_ACCESS_TOKEN',
    note: 'Trae ACP 鉴权 (CLI 登录令牌)',
  },
  opencode: {
    key: 'GRASP_OPENCODE_API_KEY',
    alt: 'OPENCODE_API_KEY',
    note: 'OpenCode API Key 鉴权',
  },
}

/** Resolve apply guide for the current Backend (+ CodeBuddy/Trae site when applicable). */
export function authGuideFor(backend: BackendId, region = ''): BackendAuthGuide {
  if (backend === 'codex') return { backend, keys: [], pathStepKeys: [], links: [] }
  if (backend === 'cursor') return CURSOR_GUIDE
  if (backend === 'claude_code') return CLAUDE_GUIDE
  if (backend === 'trae') return TRAE_GUIDE
  if (backend === 'opencode') return OPENCODE_GUIDE

  const siteLink = CODEBUDDY_SITE_LINKS[region] || CODEBUDDY_SITE_LINKS.public
  return {
    ...CODEBUDDY_BASE,
    links: [
      siteLink,
      CODEBUDDY_SITE_LINKS.public,
      CODEBUDDY_SITE_LINKS.internal,
    ].filter((link, index, arr) => arr.findIndex((x) => x.url === link.url) === index),
  }
}

/** Relative path for backend config file written into Agent workspace/files. */
export const AGENT_SETTINGS_REL_PATH = 'settings.json'
export const AGENT_OPENCODE_CONFIG_REL_PATH = 'opencode.json'

export function agentConfigRelPath(backend: BackendId): string {
  return backend === 'opencode' ? AGENT_OPENCODE_CONFIG_REL_PATH : AGENT_SETTINGS_REL_PATH
}

/** Absolute configRoot path for settings.json (UI display). */
export function settingsFileAbsPath(configRoot: string, backend: BackendId = 'cursor'): string {
  const root = configRoot.trim() || '/root/.cursor'
  return `${root}/${agentConfigRelPath(backend)}`
}

/** Default JSON placeholder when user switches to custom config mode. */
export function defaultSettingsPlaceholder(backend: BackendId): string {
  switch (backend) {
    case 'claude_code':
      return JSON.stringify({ env: { ANTHROPIC_API_KEY: 'sk-ant-...' } }, null, 2)
    case 'codebuddy':
      return JSON.stringify({ env: { CODEBUDDY_API_KEY: 'your-key' } }, null, 2)
    case 'cursor':
      return JSON.stringify({ env: { CURSOR_API_KEY: 'your-key' } }, null, 2)
    case 'trae':
      return JSON.stringify({ env: { TRAECLI_PERSONAL_ACCESS_TOKEN: 'trae-lt-...' } }, null, 2)
    case 'opencode':
      return JSON.stringify(
        {
          $schema: 'https://opencode.ai/config.json',
          model: 'openai/gpt-4.1',
          provider: {
            openai: {
              options: { apiKey: '{env:OPENCODE_API_KEY}' },
            },
          },
        },
        null,
        2,
      )
    default:
      return '{\n  \n}'
  }
}

/** i18n key for backend-specific custom-config guidance (optional). */
export function customConfigNoteKey(backend: BackendId): string {
  switch (backend) {
    case 'claude_code':
      return 'pages.agentStudio.wizard.apiKey.customConfig.notes.claude'
    case 'codebuddy':
      return 'pages.agentStudio.wizard.apiKey.customConfig.notes.codebuddy'
    case 'opencode':
      return 'pages.agentStudio.wizard.apiKey.customConfig.notes.opencode'
    default:
      return 'pages.agentStudio.wizard.apiKey.customConfig.notes.generic'
  }
}

/** True when any primary/alias auth key for the backend has a non-empty value. */
export function hasAuthKeyConfigured(
  env: Record<string, string> | { k: string; v: string }[],
  backend: BackendId,
): boolean {
  const rec = Array.isArray(env)
    ? Object.fromEntries(env.filter((e) => e.k.trim()).map((e) => [e.k.trim(), e.v]))
    : env
  const guide = authGuideFor(backend)
  const hint = BACKEND_AUTH_HINTS[backend]
  const keys = new Set<string>([hint.key, ...(hint.alt ? [hint.alt] : [])])
  for (const spec of guide.keys) {
    keys.add(spec.key)
    if (spec.alt) keys.add(spec.alt)
  }
  // Trae also accepts legacy TRAE_API_KEY alias at runtime.
  if (backend === 'trae') keys.add('TRAE_API_KEY')
  return [...keys].some((k) => (rec[k] ?? '').trim() !== '')
}
