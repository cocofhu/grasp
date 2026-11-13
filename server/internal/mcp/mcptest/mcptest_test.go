package mcptest

import (
	"testing"

	"github.com/cocofhu/grasp/internal/mcp"
)

func TestGrantAndEnd(t *testing.T) {
	h := mcp.NewHost(nil)
	tok := Grant(h, "r")
	other := Grant(h, "o")
	if !h.AuthorizeRun("r", tok) || h.AuthorizeRun("r", other) {
		t.Fatal("granted scope must accept only its own token")
	}
	h.SetActiveNode("r", "n", nil)
	End(h, "r")
	if h.AuthorizeRun("r", tok) || h.ActiveNode("r") != "mcp" {
		t.Fatal("ended scope must reject its token and drop host state")
	}
	if !h.AuthorizeRun("o", other) {
		t.Fatal("ending one scope affected another")
	}
}
