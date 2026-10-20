package services

import (
	"testing"

	"github.com/cocofhu/grasp/internal/config"
)

func TestBuildTestSchedulerMCPSpec(t *testing.T) {
	config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://spa.example.com"}})
	defer config.StoreConfig(nil)

	spec := BuildTestSchedulerMCPSpec("agent-a", "tok")
	if spec.Name != TaskSchedulerMCP {
		t.Fatalf("name = %q", spec.Name)
	}
	if spec.URL == "" || spec.Headers["Authorization"] != "Bearer tok" {
		t.Fatalf("spec = %+v", spec)
	}
	if got := BuildTestSchedulerMCPSpec("", "tok"); got.Name != "" {
		t.Fatalf("empty agent should yield empty spec: %+v", got)
	}
}

func TestTestMcpVarsScheduler(t *testing.T) {
	config.StoreConfig(&config.Config{Server: config.ServerConfig{MCPAdvertise: "http://spa.example.com"}})
	defer config.StoreConfig(nil)
	s := &SandboxService{mcpEndpoint: "http://spa.example.com"}

	vars := s.testMcpVars("run1", "tok", "agent-a")
	if vars["GRASP_SCHEDULER_TOKEN"] != "tok" {
		t.Fatalf("token = %q", vars["GRASP_SCHEDULER_TOKEN"])
	}
	if vars["GRASP_SCHEDULER_URL"] == "" {
		t.Fatalf("scheduler url missing: %+v", vars)
	}
	noProfile := s.testMcpVars("run1", "tok", "")
	if _, ok := noProfile["GRASP_SCHEDULER_URL"]; ok {
		t.Fatalf("no profile should omit scheduler url: %+v", noProfile)
	}
}
