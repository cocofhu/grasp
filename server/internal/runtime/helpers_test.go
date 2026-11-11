package runtime

import "github.com/cocofhu/grasp/internal/sandbox"

func (c *acpProvider) registerLive(req NodeReq, sb *sandbox.Sandbox, acp *sandbox.ACPClient) {
	c.mu.Lock()
	c.registerLiveLocked(req, sb, acp)
}
