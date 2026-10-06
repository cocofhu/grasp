<script lang="ts">
const STRUCTURED_ARTIFACT_NAMES = new Set([
  'clarified_requirement.json',
  'research.json',
  'root_cause.json',
  'plan.json',
  'implementation_result.json',
  'test_result.json',
  'review.json',
  'merge_request.json',
  'preflight.json',
])

// Feedback ledger products are matched by prefix. New ReAct products use one
// stable feedback.<kind>.<node>.i<n>.json name per execution while gate/preview
// products use the i<n>r<n> form.
const FEEDBACK_INDEX_NAME = 'feedback_index.json'
const FEEDBACK_PREFIX = 'feedback.'

export function isFeedbackArtifactName(name: string): boolean {
  return name === FEEDBACK_INDEX_NAME || name.startsWith(FEEDBACK_PREFIX)
}

export function isStructuredArtifactName(name: string): boolean {
  return STRUCTURED_ARTIFACT_NAMES.has(name) || isFeedbackArtifactName(name)
}
</script>

<script setup lang="ts">
import { computed } from 'vue'
import PlanView from './PlanView.vue'
import ClarifiedRequirementView from './product/ClarifiedRequirementView.vue'
import ResearchView from './product/ResearchView.vue'
import RootCauseView from './product/RootCauseView.vue'
import TestResultView from './product/TestResultView.vue'
import ReviewView from './product/ReviewView.vue'
import ImplementationResultView from './product/ImplementationResultView.vue'
import MergeRequestView from './product/MergeRequestView.vue'
import FeedbackLedgerView from './product/FeedbackLedgerView.vue'
import PreflightView from './product/PreflightView.vue'

import type { Artifact } from '@/lib/shared/types'

// Shared dispatcher: given a reserved artifact file name and its parsed JSON,
// render the matching structured view. Used by both the node "产物" tab and the
// human_gate body so they share one rendering path.
const props = defineProps<{
  name: string
  doc: any
  accent?: string
  runId?: string
  artifacts?: Artifact[]
  /** Live run status for test_result screenshot error gating; omit ⇒ terminal default. */
  runStatus?: string
}>()

const isFeedback = computed(() => isFeedbackArtifactName(props.name))
</script>

<template>
  <ClarifiedRequirementView v-if="name === 'clarified_requirement.json'" :doc="doc" :accent="accent" />
  <PreflightView v-else-if="name === 'preflight.json'" :doc="doc" :accent="accent" />
  <PlanView v-else-if="name === 'plan.json'" :doc="doc" :accent="accent" :artifacts="artifacts" />
  <ImplementationResultView v-else-if="name === 'implementation_result.json'" :doc="doc" :accent="accent" />
  <MergeRequestView v-else-if="name === 'merge_request.json'" :doc="doc" :accent="accent" />
  <ResearchView v-else-if="name === 'research.json'" :doc="doc" :accent="accent" />
  <RootCauseView v-else-if="name === 'root_cause.json'" :doc="doc" :accent="accent" :artifacts="artifacts" />
  <TestResultView
    v-else-if="name === 'test_result.json'"
    :doc="doc"
    :accent="accent"
    :run-id="runId"
    :artifacts="artifacts"
    :run-status="runStatus"
  />
  <ReviewView v-else-if="name === 'review.json'" :doc="doc" />
  <FeedbackLedgerView
    v-else-if="isFeedback"
    :name="name"
    :doc="doc"
    :run-id="runId"
    :artifacts="artifacts"
  />
</template>
