import { CLARIFY_CAPS, IMPLEMENT_CAPS, TEST_REVIEW_CAPS } from '../src/test/capsFixtures'

export const CANVAS_AGENTS = [
  { name: '需求澄清', templateId: 'clarify', capabilities: CLARIFY_CAPS },
  { name: '实现', templateId: 'implement', capabilities: IMPLEMENT_CAPS },
  { name: '测试评审', templateId: 'test_review', capabilities: TEST_REVIEW_CAPS },
  // Same capabilities as 实现, but no template id, so the default graph still picks 实现.
  { name: '交付', capabilities: IMPLEMENT_CAPS },
]
