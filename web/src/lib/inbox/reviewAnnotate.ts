import { inject, provide, type InjectionKey } from 'vue'

// A review-annotate channel lets deeply-nested product views (requirement /
// structured-artifact cards) offer a hover "⤴ 标注" affordance that
// stages a precise JSON-path (or DOM selector) chip onto the review composer,
// without threading callbacks through every intermediate component.
export interface ReviewAnnotateApi {
  // enabled gates whether descendant views render the ⤴ pick affordance.
  enabled: boolean
  // annotate stages one field/element/quote reference for the next review reply.
  annotate: (ann: {
    jsonPath?: string
    selector?: string
    label?: string
    quote?: string
    truncated?: boolean
  }) => void
}

const KEY: InjectionKey<ReviewAnnotateApi> = Symbol('reviewAnnotate')

/** Provide an annotate channel to descendant product views (review mode only). */
export function provideReviewAnnotate(api: ReviewAnnotateApi) {
  provide(KEY, api)
}

/** Inject the annotate channel; null when not inside a review panel. */
export function useReviewAnnotate(): ReviewAnnotateApi | null {
  return inject(KEY, null)
}
