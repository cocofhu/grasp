/**
 * Form descriptors for the nine structured run artifacts. One generic renderer
 * walks these; toForm/fromForm convert between the stored JSON and the form
 * model without dropping fields the form does not know about.
 */

/** Structured artifacts: form view or raw JSON. */
export type ArtifactEditorMode = 'form' | 'json'

export type FieldKind =
  | 'text'
  | 'textarea'
  | 'code'
  | 'enum'
  | 'bool'
  /** Optional boolean that must stay absent when unset (Go *bool). */
  | 'tri'
  | 'number'
  | 'strings'
  | 'list'
  | 'object'
  /** Server-computed value: shown, never edited, preserved as-is. */
  | 'auto'

export interface FieldDesc {
  key: string
  kind: FieldKind
  required?: boolean
  options?: readonly string[]
  /** list / object children. */
  fields?: readonly FieldDesc[]
  /** list: child key used as the item card title. */
  titleKey?: string
  /** list: read-only id shown on each item (server assigns / renumbers ids). */
  idKey?: string
  /** object: may be absent entirely (Go pointer). */
  optional?: boolean
  /** i18n key under pages.artifactEditor for a note under the field. */
  hint?: string
}

export interface ArtifactFormDesc {
  name: string
  fields: readonly FieldDesc[]
}

type Json = Record<string, unknown>

const t = (key: string, extra: Partial<FieldDesc> = {}): FieldDesc => ({ key, kind: 'text', ...extra })
const ta = (key: string, extra: Partial<FieldDesc> = {}): FieldDesc => ({ key, kind: 'textarea', ...extra })
const strs = (key: string, extra: Partial<FieldDesc> = {}): FieldDesc => ({ key, kind: 'strings', ...extra })
const en = (key: string, options: readonly string[], extra: Partial<FieldDesc> = {}): FieldDesc => ({
  key,
  kind: 'enum',
  options,
  ...extra,
})
const list = (key: string, fields: readonly FieldDesc[], extra: Partial<FieldDesc> = {}): FieldDesc => ({
  key,
  kind: 'list',
  fields,
  ...extra,
})

const PLAN_STATUS = ['pending', 'in_progress', 'done'] as const

const DIAGRAM_FIELDS: readonly FieldDesc[] = [
  t('title'),
  t('kind'),
  t('scope'),
  t('format'),
  { key: 'source', kind: 'code', required: true },
  t('fallback_artifact'),
  t('caption'),
]

const diagrams = (key = 'diagrams', fields: readonly FieldDesc[] = DIAGRAM_FIELDS): FieldDesc =>
  list(key, fields, { titleKey: 'title' })

const PLAN_FORM: ArtifactFormDesc = {
  name: 'plan.json',
  fields: [
    t('title', { required: true }),
    list(
      'goals',
      [
        t('title', { required: true }),
        ta('detail'),
        en('status', PLAN_STATUS),
        list('subgoals', [t('title', { required: true }), ta('detail'), en('status', PLAN_STATUS)], {
          titleKey: 'title',
          idKey: 'id',
        }),
      ],
      { required: true, titleKey: 'title', idKey: 'id', hint: 'planIdHint' },
    ),
    { key: 'architecture', kind: 'object', optional: true, fields: [ta('summary'), diagrams()] },
    {
      key: 'data_design',
      kind: 'object',
      optional: true,
      fields: [
        ta('summary'),
        list(
          'entities',
          [
            t('name', { required: true }),
            ta('description'),
            list(
              'fields',
              [
                t('name', { required: true }),
                t('type'),
                { key: 'pk', kind: 'tri' },
                { key: 'nullable', kind: 'tri' },
                t('fk'),
                t('description'),
              ],
              { titleKey: 'name' },
            ),
            strs('relationships'),
          ],
          { titleKey: 'name' },
        ),
        strs('relationships'),
        diagrams(),
      ],
    },
    list('interfaces', [t('name', { required: true }), t('kind'), t('direction'), ta('summary'), ta('detail'), diagrams()], {
      titleKey: 'name',
    }),
    list('components', [t('name', { required: true }), ta('responsibility'), strs('dependencies'), ta('detail'), diagrams()], {
      titleKey: 'name',
    }),
    { key: 'interaction', kind: 'object', optional: true, fields: [ta('summary'), diagrams()] },
    ta('test_design'),
  ],
}

const TEST_RESULT_FORM: ArtifactFormDesc = {
  name: 'test_result.json',
  fields: [
    ta('summary', { required: true }),
    { key: 'passed', kind: 'auto' },
    { key: 'failed', kind: 'auto' },
    { key: 'skipped', kind: 'auto' },
    ta('assessment'),
    ta('variances'),
    list('cases', [t('name', { required: true }), en('status', ['passed', 'failed', 'skipped']), ta('detail')], {
      titleKey: 'name',
      idKey: 'id',
    }),
    list(
      'plan_coverage',
      [t('plan_id', { required: true }), t('title'), { key: 'passed', kind: 'bool', required: true }, ta('evidence'), strs('cases')],
      { titleKey: 'plan_id' },
    ),
    list(
      'defects',
      [t('title', { required: true }), en('severity', ['critical', 'high', 'medium', 'low']), t('status'), ta('detail')],
      { titleKey: 'title', idKey: 'id' },
    ),
    list('screenshots', [t('artifact', { required: true }), t('mimeType'), t('caption')], { titleKey: 'artifact' }),
  ],
}

const CLARIFIED_FORM: ArtifactFormDesc = {
  name: 'clarified_requirement.json',
  fields: [
    t('title', { required: true }),
    ta('summary', { required: true }),
    ta('background', { required: true }),
    en('work_kind', ['bug', 'feature', 'other']),
    strs('goals', { required: true }),
    strs('success_metrics'),
    strs('in_scope', { required: true }),
    strs('out_of_scope', { required: true }),
    list('personas', [t('name', { required: true }), ta('description'), strs('goals')], { titleKey: 'name', idKey: 'id' }),
    list('user_scenarios', [t('name', { required: true }), t('actor'), t('trigger'), strs('flow'), ta('outcome')], {
      titleKey: 'name',
      idKey: 'id',
    }),
    list(
      'functional_requirements',
      [
        t('title', { required: true }),
        ta('detail', { required: true }),
        en('priority', ['must', 'should', 'could']),
        strs('acceptance_criteria', { required: true }),
        strs('scenario_ids'),
      ],
      { required: true, titleKey: 'title', idKey: 'id' },
    ),
    list(
      'non_functional_requirements',
      [
        en('category', ['performance', 'security', 'usability', 'reliability', 'compatibility', 'other']),
        ta('detail', { required: true }),
        t('metric'),
      ],
      { titleKey: 'category', idKey: 'id' },
    ),
    list(
      'external_interfaces',
      [
        t('name', { required: true }),
        en('kind', ['user', 'system', 'hardware', 'software', 'communication']),
        en('direction', ['in', 'out', 'both']),
        ta('description'),
      ],
      { titleKey: 'name', idKey: 'id' },
    ),
    list('data_entities', [t('name', { required: true }), ta('description'), strs('attributes')], {
      titleKey: 'name',
      idKey: 'id',
    }),
    strs('business_rules'),
    strs('edge_cases'),
    strs('assumptions', { required: true }),
    strs('dependencies', { required: true }),
    strs('constraints', { required: true }),
    strs('limitations'),
    list('risks', [ta('description', { required: true }), ta('mitigation')], { titleKey: 'description', idKey: 'id' }),
    list('glossary', [t('term', { required: true }), ta('definition')], { titleKey: 'term' }),
    strs('open_questions'),
  ],
}

const RESEARCH_FORM: ArtifactFormDesc = {
  name: 'research.json',
  fields: [
    t('title'),
    ta('summary', { required: true }),
    list('questions', [t('question', { required: true }), ta('answer')], { titleKey: 'question', idKey: 'id' }),
    list('findings', [t('title', { required: true }), ta('detail')], { titleKey: 'title', idKey: 'id' }),
    ta('recommendation'),
    strs('references'),
    strs('follow_ups'),
  ],
}

const ROOT_CAUSE_FORM: ArtifactFormDesc = {
  name: 'root_cause.json',
  fields: [
    t('title'),
    ta('summary', { required: true }),
    ta('symptom'),
    ta('expected'),
    ta('actual'),
    strs('reproduction'),
    ta('impact'),
    ta('root_cause', { required: true }),
    list('evidence', [t('title', { required: true }), ta('detail')], { titleKey: 'title' }),
    diagrams('diagrams', [t('title'), t('kind'), t('format'), { key: 'source', kind: 'code', required: true }, t('caption')]),
    strs('ruled_out'),
    strs('contributing_factors'),
    strs('affected_scope'),
    strs('causal_chain'),
  ],
}

const IMPLEMENTATION_FORM: ArtifactFormDesc = {
  name: 'implementation_result.json',
  fields: [
    ta('summary', { required: true }),
    t('change_type'),
    list('changed_areas', [t('title', { required: true }), ta('detail')], { titleKey: 'title' }),
    strs('tests'),
    strs('breaking_changes'),
    strs('follow_ups'),
  ],
}

const REVIEW_FORM: ArtifactFormDesc = {
  name: 'review.json',
  fields: [
    ta('summary', { required: true }),
    en('verdict', ['approve', 'approve_with_comments', 'request_changes', 'reject'], { required: true }),
    list(
      'findings',
      [
        t('title', { required: true }),
        en('severity', ['critical', 'high', 'medium', 'low'], { required: true }),
        t('file'),
        { key: 'line', kind: 'number' },
        ta('detail'),
        ta('suggestion'),
      ],
      { titleKey: 'title', idKey: 'id' },
    ),
    strs('action_items'),
  ],
}

const MERGE_REQUEST_FORM: ArtifactFormDesc = {
  name: 'merge_request.json',
  fields: [
    ta('summary'),
    list(
      'items',
      [
        t('repo', { required: true }),
        t('sourceBranch'),
        t('targetBranch'),
        t('url'),
        en('provider', ['github', 'gitlab', 'other']),
        en('state', ['created', 'reused', 'merged', 'unsupported']),
        ta('note'),
      ],
      { required: true, titleKey: 'repo' },
    ),
  ],
}

const PREFLIGHT_FORM: ArtifactFormDesc = {
  name: 'preflight.json',
  fields: [
    ta('summary'),
    { key: 'confirmed', kind: 'bool', required: true },
    list(
      'fields',
      [
        t('name', { required: true }),
        t('label'),
        t('value'),
        { key: 'verified', kind: 'bool' },
        en('verification', ['sandbox_probe', 'user_attested', 'mixed']),
        en('source', ['form', 'choice', 'chat']),
        ta('notes'),
      ],
      { titleKey: 'label' },
    ),
    strs('unresolved'),
  ],
}

export const ARTIFACT_FORMS: Readonly<Record<string, ArtifactFormDesc>> = Object.fromEntries(
  [
    CLARIFIED_FORM,
    RESEARCH_FORM,
    ROOT_CAUSE_FORM,
    PLAN_FORM,
    IMPLEMENTATION_FORM,
    TEST_RESULT_FORM,
    REVIEW_FORM,
    MERGE_REQUEST_FORM,
    PREFLIGHT_FORM,
  ].map((f) => [f.name, f]),
)

export function artifactFormFor(name: string | null | undefined): ArtifactFormDesc | null {
  return ARTIFACT_FORMS[String(name || '')] ?? null
}

function isObj(v: unknown): v is Json {
  return !!v && typeof v === 'object' && !Array.isArray(v)
}

function clone<T>(v: T): T {
  return v === undefined ? v : (JSON.parse(JSON.stringify(v)) as T)
}

/** Empty value for a new item / missing field in the form model. */
export function emptyFieldValue(f: FieldDesc): unknown {
  switch (f.kind) {
    case 'text':
    case 'textarea':
    case 'code':
    case 'enum':
      return ''
    case 'bool':
      return false
    case 'strings':
    case 'list':
      return []
    case 'object':
      return fillObject({}, f.fields ?? [])
    default:
      return undefined
  }
}

export function emptyListItem(f: FieldDesc): Json {
  return fillObject({}, f.fields ?? [])
}

function fillObject(obj: Json, fields: readonly FieldDesc[]): Json {
  for (const f of fields) {
    const v = obj[f.key]
    if (f.kind === 'list') {
      obj[f.key] = Array.isArray(v) ? v.map((it) => (isObj(it) ? fillObject(it, f.fields ?? []) : it)) : []
    } else if (f.kind === 'object') {
      if (isObj(v)) fillObject(v, f.fields ?? [])
      else if (!f.optional) obj[f.key] = fillObject({}, f.fields ?? [])
    } else if (f.kind === 'strings') {
      obj[f.key] = Array.isArray(v) ? v.map((s) => String(s ?? '')) : []
    } else if (v === undefined || v === null) {
      const e = emptyFieldValue(f)
      if (e !== undefined) obj[f.key] = e
    }
  }
  return obj
}

function pruneObject(obj: Json, fields: readonly FieldDesc[]): Json {
  for (const f of fields) {
    if (!(f.key in obj)) continue
    const v = obj[f.key]
    switch (f.kind) {
      case 'text':
      case 'textarea':
      case 'code':
      case 'enum':
        if (!f.required && (v === '' || v === null || v === undefined)) delete obj[f.key]
        break
      case 'bool':
        if (!f.required && v !== true) delete obj[f.key]
        break
      case 'tri':
        if (v === null || v === undefined) delete obj[f.key]
        break
      case 'number': {
        const n = typeof v === 'number' ? v : Number(v)
        if (v === '' || v === null || v === undefined || !Number.isFinite(n) || (!f.required && n === 0)) delete obj[f.key]
        else obj[f.key] = n
        break
      }
      case 'strings': {
        const arr = Array.isArray(v) ? v.map((s) => String(s ?? '')).filter((s) => s.trim() !== '') : []
        if (!arr.length && !f.required) delete obj[f.key]
        else obj[f.key] = arr
        break
      }
      case 'list': {
        const arr = Array.isArray(v) ? v.map((it) => (isObj(it) ? pruneObject(it, f.fields ?? []) : it)) : []
        if (!arr.length && !f.required) delete obj[f.key]
        else obj[f.key] = arr
        break
      }
      case 'object':
        if (isObj(v)) pruneObject(v, f.fields ?? [])
        else if (f.optional) delete obj[f.key]
        break
      default:
        break
    }
  }
  return obj
}

/** Stored JSON → form model: deep copy with every known field present. */
export function toForm(desc: ArtifactFormDesc, doc: unknown): Json {
  return fillObject(isObj(doc) ? clone(doc) : {}, desc.fields)
}

/** Form model → JSON to save: drops empty optional fields, keeps unknown keys. */
export function fromForm(desc: ArtifactFormDesc, form: Json): Json {
  return pruneObject(clone(form), desc.fields)
}

/** Parse stored content for the form; null when it is not a JSON object. */
export function parseFormContent(desc: ArtifactFormDesc, content: string): Json | null {
  try {
    const v = JSON.parse(content || '{}')
    return isObj(v) ? toForm(desc, v) : null
  } catch {
    return null
  }
}

export function serializeForm(desc: ArtifactFormDesc, form: Json): string {
  return JSON.stringify(fromForm(desc, form), null, 2)
}

/**
 * Pick the form path a server field error belongs to: the longest
 * prefix of `field` among `paths` (segments split on `.` and `[`).
 */
export function matchFieldPath(field: string, paths: readonly string[]): string | null {
  const set = new Set(paths)
  let cur = field.trim()
  while (cur) {
    if (set.has(cur)) return cur
    const dot = cur.lastIndexOf('.')
    const br = cur.lastIndexOf('[')
    const cut = Math.max(dot, br)
    if (cut <= 0) break
    cur = cur.slice(0, cut)
  }
  return null
}
