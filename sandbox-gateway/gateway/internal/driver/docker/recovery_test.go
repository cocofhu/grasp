package docker

import (
	"errors"
	"reflect"
	"testing"

	"sandbox-gateway/internal/driver"
)

func TestCaptureSpecPreservesOriginalBindsAndRuntimeEnvironment(t *testing.T) {
	mock := newMock()
	mock.on("inspect", `[{"Config":{"Image":"original:pinned","Env":["WORKSPACE_DIR=/custom/workspace","TOKEN=a=b=c","EMPTY=","invalid"]},"HostConfig":{"Binds":["/host/data:/custom/workspace:rw","/host/agent:/root/.agent:ro"]},"Mounts":[{"Type":"volume","Source":"/var/lib/docker/volumes/anonymous/_data","Destination":"/scratch"}]}]`, nil)
	drv := New(Options{})
	drv.run = mock.run
	original := driver.Spec{ID: "existing", Image: "changed-default", WorkspaceDir: "/changed/default", Config: &driver.ConfigInject{HostPath: "/wrong/config"}, Ports: []int{8765, 22}, InternalPorts: []int{9222}, Labels: map[string]string{"owner": "grasp"}, Resources: driver.Resources{CPUCores: 4, MemoryMB: 8192, DiskGi: 50}}
	got, err := drv.CaptureSpec(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	if got.Image != "original:pinned" || got.WorkspaceDir != "/custom/workspace" || got.Env["TOKEN"] != "a=b=c" || got.Config != nil ||
		!reflect.DeepEqual(got.Mounts, []string{"/host/data:/custom/workspace:rw", "/host/agent:/root/.agent:ro"}) {
		t.Fatalf("original host mounts or environment lost: %+v", got)
	}
	if got.ID != original.ID || !reflect.DeepEqual(got.Ports, original.Ports) || !reflect.DeepEqual(got.InternalPorts, original.InternalPorts) || got.Resources != original.Resources || !reflect.DeepEqual(got.Labels, original.Labels) {
		t.Fatalf("capture discarded control-plane identity/profile: %+v", got)
	}
	if len(mock.callsWith("rm")) > 0 || len(mock.callsWith("run")) > 0 || !reflect.DeepEqual(mock.lastCall(), []string{"inspect", "sbx-existing"}) {
		t.Fatalf("capturing source spec modified the workload: %v", mock.lastCall())
	}
}

func TestCaptureSpecRefusesUnreliableSourceInputs(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		err          error
	}{
		{"daemon-error", "", errors.New("daemon unavailable")},
		{"malformed", "{", nil},
		{"missing", "[]", nil},
		{"ambiguous", "[{},{}]", nil},
		{"missing-image", `[{"Config":{"Env":["WORKSPACE_DIR=/workspace"]}}]`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mock := newMock()
			mock.on("inspect", tc.output, tc.err)
			drv := New(Options{})
			drv.run = mock.run
			if _, err := drv.CaptureSpec(t.Context(), driver.Spec{ID: "source"}); err == nil {
				t.Fatal("unreliable source inputs accepted")
			}
			if len(mock.callsWith("rm")) != 0 {
				t.Fatal("failed inspection deleted original")
			}
		})
	}
}
