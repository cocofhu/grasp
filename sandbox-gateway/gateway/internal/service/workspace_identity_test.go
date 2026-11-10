package service

import (
	"context"
	"testing"
)

func TestWorkspaceRuntimeIdentityUsesThePersistedGatewayResource(t *testing.T) {
	svc, drv, store := testService(t)
	input := map[string]string{"GRASP_SANDBOX_RESOURCE_ID": "caller-placeholder", "OTHER": "retained"}
	sb, err := svc.Create(context.Background(), CreateRequest{Env: input})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	stored, err := store.Get(context.Background(), sb.ID)
	if err != nil || stored.Env()["GRASP_SANDBOX_RESOURCE_ID"] != sb.ID || stored.Env()["OTHER"] != "retained" {
		t.Fatalf("runtime identity differs from durable resource: %+v %v", stored, err)
	}
	if spec := drv.LastCreate(); spec == nil || spec.Env["GRASP_SANDBOX_RESOURCE_ID"] != sb.ID {
		t.Fatal("native provisioning did not receive the durable identity")
	}
	if input["GRASP_SANDBOX_RESOURCE_ID"] != "caller-placeholder" {
		t.Fatal("gateway mutated caller environment")
	}
	if err := svc.Stop(context.Background(), sb.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(context.Background(), sb.ID); err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	resumed, err := store.Get(context.Background(), sb.ID)
	if err != nil || resumed.Env()["GRASP_SANDBOX_RESOURCE_ID"] != sb.ID {
		t.Fatalf("resume changed the retained resource identity: %+v %v", resumed, err)
	}
}
