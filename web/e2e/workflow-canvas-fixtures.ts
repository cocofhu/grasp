import { CLARIFY_CAPS, IMPLEMENT_CAPS, TEST_REVIEW_CAPS } from '../src/test/capsFixtures'

export const CANVAS_AGENTS = [
  { name: '需求澄清', projectId: 'proj-1', templateId: 'clarify', capabilities: CLARIFY_CAPS },
  { name: '实现', projectId: 'proj-1', templateId: 'implement', capabilities: IMPLEMENT_CAPS },
  { name: '测试评审', projectId: 'proj-1', templateId: 'test_review', capabilities: TEST_REVIEW_CAPS },
]
