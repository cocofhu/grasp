<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import Icon from '@/components/ui/Icon.vue'
import type { MessageReference } from '@/lib/api/apiTypes'
import { referenceHref, referenceIcon, referenceTone } from '@/lib/solo/referenceDisplay'
import { referenceLabel } from '@/lib/solo/referenceTokens'

const props = defineProps<{
  reference: MessageReference
  defaultRepo?: string
  removable?: boolean
  /** History chips open their target; composer chips do not. */
  link?: boolean
}>()
const emit = defineEmits<{ (e: 'remove'): void }>()
const { t } = useI18n()

const label = computed(() => referenceLabel(props.reference, props.defaultRepo))
const icon = computed(() => referenceIcon(props.reference))
const tone = computed(() => referenceTone(props.reference))
const href = computed(() => (props.link ? referenceHref(props.reference) : ''))
const external = computed(() => props.reference.kind !== 'solo')
/** Only the attributes the chosen element needs: an explicit undefined href would override RouterLink's. */
const linkAttrs = computed(() => {
  if (!href.value) return {}
  if (!external.value) return { to: href.value }
  return { href: href.value, target: '_blank', rel: 'noopener noreferrer' }
})
const warn = computed(() => props.reference.access === 'denied' || !!props.reference.error)
const tip = computed(() => {
  const r = props.reference
  if (r.error === 'deleted') return t('pages.solo.reference.deleted')
  if (r.access === 'denied') return t('pages.solo.reference.denied', { reason: r.error || '' })
  if (r.access === 'unchecked') return t('pages.solo.reference.unchecked')
  const parts = [r.title || label.value]
  if (r.kind === 'solo' && typeof r.messages === 'number') parts.push(t('pages.solo.reference.pinned', { n: r.messages }))
  return parts.join(' · ')
})
</script>

<template>
  <component
    :is="!href ? 'span' : external ? 'a' : RouterLink"
    v-bind="linkAttrs"
    class="ref-chip"
    :class="[`ref-chip--${reference.kind}`, { 'ref-chip--warn': warn }]"
    :title="tip"
    data-testid="solo-reference-chip"
  >
    <span class="ref-chip__icon" :class="tone" :data-icon="icon"><Icon :name="icon" :size="12" :interactive="false" /></span>
    <span class="truncate">{{ label }}</span>
    <button
      v-if="removable"
      type="button"
      class="ref-chip__x"
      :aria-label="t('pages.solo.reference.remove', { label })"
      @mousedown.prevent
      @click.stop.prevent="emit('remove')"
    ><Icon name="close" :size="10" /></button>
  </component>
</template>

<style scoped>
.ref-chip {
  display: inline-flex;
  max-width: 260px;
  height: 22px;
  align-items: center;
  gap: 4px;
  border-radius: 6px;
  background: rgb(var(--c-accent) / 0.12);
  padding: 0 7px;
  color: rgb(var(--c-txt));
  font-size: 12px;
  vertical-align: middle;
}
a.ref-chip:hover,
.ref-chip[href]:hover {
  background: rgb(var(--c-accent) / 0.2);
}
.ref-chip--solo {
  background: rgb(var(--c-ok) / 0.12);
}
.ref-chip--warn {
  background: rgb(var(--c-err) / 0.1);
  color: rgb(var(--c-txt2));
}
.ref-chip__icon {
  display: grid;
  flex: none;
  place-items: center;
}
/* Parents list chips in <TransitionGroup name="ref-chip">. */
.ref-chip-enter-active {
  transition: opacity 220ms cubic-bezier(0.16, 1, 0.3, 1), transform 220ms cubic-bezier(0.16, 1, 0.3, 1);
}
.ref-chip-leave-active {
  pointer-events: none;
  transition: opacity 140ms ease, transform 140ms ease;
}
.ref-chip-enter-from,
.ref-chip-leave-to {
  opacity: 0;
  transform: scale(0.86);
}
.ref-chip-move {
  transition: transform 200ms cubic-bezier(0.16, 1, 0.3, 1);
}
.ref-chip {
  transform-origin: left center;
}
@media (prefers-reduced-motion: reduce) {
  .ref-chip-enter-active,
  .ref-chip-leave-active,
  .ref-chip-move {
    transition: none;
  }
}
.ref-chip__x {
  display: grid;
  width: 14px;
  height: 14px;
  margin-right: -3px;
  place-items: center;
  border-radius: 4px;
  color: rgb(var(--c-txt3));
  transition: background 150ms ease, color 150ms ease;
}
.ref-chip__x:hover {
  background: rgb(var(--c-txt) / 0.08);
  color: rgb(var(--c-txt));
}
</style>
