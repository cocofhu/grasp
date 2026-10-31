/**
 * The newest server dialogue per node, shared by every surface that draws it.
 * WebSocket `type:"dialogue"` frames and REST snapshots both go through
 * ingest; a tail that does not fit triggers one coalesced REST refetch.
 */
import { computed, shallowReactive, type ComputedRef } from 'vue'
import { runsClient } from '../api/clients/runsClient'
import {
  addOptimistic as addTo,
  applyFrame,
  pendingOptimistic,
  rejectOptimistic as rejectFrom,
  render,
  type DialogueState,
  type DialogueView,
  type OptimisticSend,
} from './dialogueState'

export type DialogueFetcher = (runId: string, nodeId: string) => Promise<DialogueState | null | undefined>

const states = shallowReactive(new Map<string, DialogueState>())
const optimistic = shallowReactive(new Map<string, OptimisticSend[]>())
const fetchers = new Map<string, DialogueFetcher>()
let defaultFetcher: DialogueFetcher = (runId, nodeId) => runsClient.nodeDialogue(runId, nodeId)

type Resync = { inflight: boolean; again: boolean; tries: number; timer?: ReturnType<typeof setTimeout> }
const resyncs = new Map<string, Resync>()
const RESYNC_BASE_MS = 300
const RESYNC_MAX_MS = 8000
const RESYNC_TRIES = 6

export function dialogueKey(runId: string, nodeId: string): string {
  return `${runId}:${nodeId}`
}

function splitKey(key: string): [string, string] {
  const i = key.indexOf(':')
  return i < 0 ? [key, ''] : [key.slice(0, i), key.slice(i + 1)]
}

function isDialogue(v: unknown): v is DialogueState {
  const f = v as DialogueState | null
  return !!f && f.type === 'dialogue' && typeof f.seq === 'number' && typeof f.epoch === 'number' && Array.isArray(f.rows)
}

/**
 * Apply one dialogue frame. key defaults to runId:nodeId of the frame; the
 * share page passes its own (its frames carry no run id). Returns whether the
 * frame changed what is shown.
 */
export function ingestDialogue(frame: unknown, key?: string): boolean {
  if (!isDialogue(frame)) return false
  // A share visitor's lane is its own chat; only its page (explicit key) takes it.
  if (key === undefined && frame.lane) return false
  const k = key ?? dialogueKey(frame.runId, frame.nodeId)
  const r = applyFrame(states.get(k) ?? null, frame)
  if (r.resync) {
    requestDialogueResync(k)
    return false
  }
  if (!r.applied || !r.state) return false
  states.set(k, Object.freeze(r.state))
  settleOptimistic(k)
  return true
}

/** REST snapshots keyed by node (run detail / inbox context `dialogueByNode`). */
export function ingestDialogues(byNode: Record<string, unknown> | null | undefined, runId?: string) {
  if (!byNode) return
  for (const [nodeId, frame] of Object.entries(byNode)) {
    if (!isDialogue(frame)) continue
    ingestDialogue(frame, dialogueKey(runId || frame.runId, nodeId))
  }
}

function settleOptimistic(key: string) {
  const list = optimistic.get(key)
  if (!list?.length) return
  const left = pendingOptimistic(states.get(key) ?? null, list)
  if (left.length !== list.length) optimistic.set(key, left)
}

export function getDialogue(key: string): DialogueState | null {
  return states.get(key) ?? null
}

export function hasDialogue(key: string): boolean {
  return states.has(key)
}

/** This page's sends for key that the server state does not list yet. */
export function optimisticSends(key: string): OptimisticSend[] {
  return optimistic.get(key) ?? []
}

export function addOptimisticSend(key: string, send: OptimisticSend) {
  optimistic.set(key, addTo(optimistic.get(key) ?? [], send))
  settleOptimistic(key)
}

export function rejectOptimisticSend(key: string, clientMessageId: string) {
  const list = optimistic.get(key)
  if (list) optimistic.set(key, rejectFrom(list, clientMessageId))
}

export function dialogueView(key: string): DialogueView {
  return render(states.get(key) ?? null, optimistic.get(key) ?? [])
}

/** Reactive view of one node dialogue. */
export function useDialogue(key: () => string): ComputedRef<DialogueView> {
  return computed(() => dialogueView(key()))
}

/** Override the REST source: per key (share page), or the default for all keys. */
export function setDialogueFetcher(fetcher: DialogueFetcher | null, key?: string) {
  if (key) {
    if (fetcher) fetchers.set(key, fetcher)
    else fetchers.delete(key)
    return
  }
  defaultFetcher = fetcher ?? ((runId, nodeId) => runsClient.nodeDialogue(runId, nodeId))
}

/** Refetch this node over REST; concurrent requests coalesce, failures back off. */
export function requestDialogueResync(key: string) {
  const r = resyncs.get(key) ?? { inflight: false, again: false, tries: 0 }
  resyncs.set(key, r)
  if (r.inflight || r.timer) {
    r.again = true
    return
  }
  void runResync(key, r)
}

async function runResync(key: string, r: Resync) {
  r.inflight = true
  r.again = false
  let ok = false
  try {
    const [runId, nodeId] = splitKey(key)
    const fetcher = fetchers.get(key) ?? defaultFetcher
    const snap = await fetcher(runId, nodeId)
    if (resyncs.get(key) !== r) return
    if (isDialogue(snap)) {
      const res = applyFrame(states.get(key) ?? null, { ...snap, mode: 'full' })
      if (res.applied && res.state) {
        states.set(key, Object.freeze(res.state))
        settleOptimistic(key)
      }
    }
    ok = true
  } catch {
    ok = false
  } finally {
    r.inflight = false
  }
  if (resyncs.get(key) !== r) return
  if (ok) {
    r.tries = 0
    if (r.again) void runResync(key, r)
    else resyncs.delete(key)
    return
  }
  r.tries++
  if (r.tries >= RESYNC_TRIES) {
    resyncs.delete(key)
    return
  }
  const wait = Math.min(RESYNC_MAX_MS, RESYNC_BASE_MS * 2 ** (r.tries - 1))
  r.timer = setTimeout(() => {
    r.timer = undefined
    void runResync(key, r)
  }, wait)
}

/** Forget one node (key), every node of a run (runId prefix), or everything. */
export function clearDialogues(match?: { key?: string; runId?: string }) {
  const drop = (k: string) => {
    states.delete(k)
    optimistic.delete(k)
    const r = resyncs.get(k)
    if (r?.timer) clearTimeout(r.timer)
    resyncs.delete(k)
  }
  const keys = new Set([...states.keys(), ...optimistic.keys(), ...resyncs.keys()])
  for (const k of keys) {
    if (!match || (match.key && k === match.key) || (match.runId && k.startsWith(`${match.runId}:`))) drop(k)
  }
}
