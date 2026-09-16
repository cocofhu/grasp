/**
 * Global serial queue for mermaid.initialize / parse / render.
 *
 * Mermaid 11 keeps shared global state (defs / temp DOM). Concurrent
 * MermaidDiagram instances (PlanView architecture / data / interaction)
 * calling render at once contaminate each other's SVG. Serialize the
 * critical section across all instances; keep per-instance gen checks
 * outside this module so stale tasks can still no-op after dequeue.
 */

export type MermaidLike = {
  initialize: (config: Record<string, unknown>) => void
  parse: (source: string) => Promise<unknown>
  render: (id: string, source: string) => Promise<{ svg: string }>
}

let chain: Promise<void> = Promise.resolve()

/** Enqueue a task so only one mermaid critical section runs at a time. */
export function enqueueMermaidTask<T>(task: () => Promise<T>): Promise<T> {
  const run = chain.then(task, task)
  chain = run.then(
    () => undefined,
    () => undefined,
  )
  return run
}

/** Load mermaid once per task and run initialize+parse+render under the serial lock. */
export async function withMermaidSerial<T>(fn: (mermaid: MermaidLike) => Promise<T>): Promise<T> {
  return enqueueMermaidTask(async () => {
    const mod = await import('mermaid')
    return fn(mod.default as MermaidLike)
  })
}

/** Test helper: wait until the queue is idle (no pending critical sections). */
export function flushMermaidQueue(): Promise<void> {
  return enqueueMermaidTask(async () => undefined)
}
