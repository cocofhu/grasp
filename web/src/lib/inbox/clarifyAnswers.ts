import { computed, ref, watch, type Ref } from 'vue'
import type { ReactForm, ReactFormField, ReactOption, ReactQuestion } from '@/lib/shared/types'

/** Answer state for one ask_question round: one card per question, 上一题 / 下一个. */
export function useQuestionDeck(questions: Ref<ReactQuestion[]>) {
  // Selected option ids per question, plus optional "其他" free text.
  const sel = ref<Record<string, string[]>>({})
  const other = ref<Record<string, string>>({})
  /** Per-question "其他" checkbox — free text only counts when checked. */
  const otherChecked = ref<Record<string, boolean>>({})
  const step = ref(0)

  function isSelected(qid: string, oid: string): boolean {
    return (sel.value[qid] || []).includes(oid)
  }
  // Never auto-advances — the user moves on with 下一个.
  function toggle(q: ReactQuestion, oid: string) {
    const cur = sel.value[q.id] || []
    if (q.allowMultiple) {
      sel.value[q.id] = cur.includes(oid) ? cur.filter((x) => x !== oid) : [...cur, oid]
      return
    }
    // Single-select domain includes preset options and 「其他」. Replacing or
    // clearing a preset must drop the other checkbox and must not restore it.
    sel.value[q.id] = cur.includes(oid) ? [] : [oid]
    otherChecked.value[q.id] = false
  }
  function isOtherSelected(qid: string): boolean {
    return !!otherChecked.value[qid]
  }
  function toggleOther(q: ReactQuestion) {
    const next = !otherChecked.value[q.id]
    otherChecked.value[q.id] = next
    // Checking 「其他」 on a single-select question clears presets. Unchecking
    // leaves sel empty so the previous preset does not come back. Typing in the
    // input never checks this box; residual text stays in `other` either way.
    if (next && !q.allowMultiple) sel.value[q.id] = []
  }
  function answered(q: ReactQuestion): boolean {
    return (sel.value[q.id]?.length ?? 0) > 0 || (!!otherChecked.value[q.id] && !!other.value[q.id]?.trim())
  }
  const someAnswered = computed(() => questions.value.some(answered))

  // Auto-pick id for a question: the recommended option, or the first as a
  // fallback (mirrors the backend auto_var / SelectRecommendedOption rule).
  function autoPickId(q: ReactQuestion): string {
    const rec = q.options.find((o) => o.recommended)
    return rec ? rec.id : (q.options[0]?.id ?? '')
  }
  const hasRecommended = computed(() => questions.value.some((q) => q.options.some((o) => o.recommended)))
  function applyRecommended() {
    for (const q of questions.value) {
      const id = autoPickId(q)
      sel.value[q.id] = id ? [id] : []
      // Multi-select still fills only the one recommended (or first) preset and
      // leaves an already-checked 「其他」 alone. Single-select drops that check.
      if (!q.allowMultiple) otherChecked.value[q.id] = false
    }
  }

  // Only a genuinely new question set (different question ids) resets the deck to
  // the first card. Keyed by ids — not array identity — so that background run
  // polling (which rebuilds `turns` into fresh objects each tick) does NOT snap
  // the user back to the first question mid-answer.
  watch(
    () => questions.value.map((q) => q.id).join('|'),
    (key) => {
      step.value = 0
      if (!key) return
      // Preselect the recommended option (if any) so the user only confirms;
      // never auto-pick the first when nothing was recommended. immediate: hosts
      // mount only after the current question is already in props.
      for (const q of questions.value) {
        const rec = q.options.find((o) => o.recommended)
        if (rec && !(sel.value[q.id]?.length ?? 0)) sel.value[q.id] = [rec.id]
      }
    },
    { immediate: true },
  )
  const curQuestion = computed<ReactQuestion | null>(() => questions.value[step.value] || null)
  const isFirstCard = computed(() => step.value <= 0)
  const isLastCard = computed(() => step.value >= questions.value.length - 1)
  function prevCard() {
    if (step.value > 0) step.value--
  }
  function nextCard() {
    if (step.value < questions.value.length - 1) step.value++
  }

  function selectedDemo(q: ReactQuestion): ReactOption | null {
    return q.options.find((o) => isSelected(q.id, o.id) && !!o.demoHtml?.trim()) ?? null
  }

  /** `prefix` + one line per answered question; skipped questions are omitted. */
  function summary(prefix: string): string {
    const lines = questions.value.filter(answered).map((q) => {
      const picked = q.options.filter((o) => isSelected(q.id, o.id)).map((o) => o.label)
      const extra = otherChecked.value[q.id] ? other.value[q.id]?.trim() : ''
      if (extra) picked.push(extra)
      return `- ${q.prompt} → ${picked.join('、')}`
    })
    return prefix + '\n' + lines.join('\n')
  }
  function reset() {
    sel.value = {}
    other.value = {}
    otherChecked.value = {}
    step.value = 0
  }

  return {
    questions, sel, other, otherChecked, step, curQuestion, isFirstCard, isLastCard,
    isSelected, toggle, isOtherSelected, toggleOther, answered, someAnswered,
    hasRecommended, applyRecommended, prevCard, nextCard, selectedDemo, summary, reset,
  }
}
export type QuestionDeck = ReturnType<typeof useQuestionDeck>

export function fieldWhy(f: ReactFormField): string {
  return (f.why || f.reason || '').trim()
}

/** Input type for ask_form: text|url only — never password. */
export function formInputType(f: ReactFormField): 'text' | 'url' {
  return String(f.type || '').toLowerCase() === 'url' ? 'url' : 'text'
}

/** Field values for one ask_form round, keyed by `${formIndex}:${fieldName}` (plaintext, never masked). */
export function useFormDraft(forms: Ref<ReactForm[]>) {
  const values = ref<Record<string, string>>({})
  const fieldKey = (formIndex: number, name: string) => `${formIndex}:${name}`

  watch(
    () => forms.value.map((f, i) => `${i}:${(f.fields || []).map((x) => x.name).join(',')}`).join('|'),
    (key) => {
      if (!key) {
        values.value = {}
        return
      }
      const next: Record<string, string> = {}
      forms.value.forEach((form, fi) => {
        for (const f of form.fields || []) {
          const k = fieldKey(fi, f.name)
          next[k] = values.value[k] ?? (f.value || '')
        }
      })
      values.value = next
    },
    { immediate: true },
  )

  const filled = computed(() =>
    forms.value.some((f) => (f.fields || []).length > 0) &&
    forms.value.every((form, fi) => (form.fields || []).every((f) => (values.value[fieldKey(fi, f.name)] || '').trim())),
  )

  function summary(prefix: string): string {
    const lines = forms.value.flatMap((form, fi) => (form.fields || []).map((f) => {
      const label = (f.label || f.name || '').trim() || f.name
      return `- ${label} → ${(values.value[fieldKey(fi, f.name)] || '').trim()}`
    }))
    return prefix + '\n' + lines.join('\n')
  }
  function reset() {
    values.value = {}
  }

  return { forms, values, fieldKey, filled, summary, reset }
}
export type FormDraft = ReturnType<typeof useFormDraft>
