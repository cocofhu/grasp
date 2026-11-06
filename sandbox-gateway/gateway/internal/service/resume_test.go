package service

import (
	"context"
	"errors"
	"testing"

	"sandbox-gateway/internal/driver"
)

type preparingDriver struct {
	driver.Driver
	options driver.StartOptions
	fail    bool
	started bool
}

func (d *preparingDriver) PrepareStart(_ context.Context, _ string, opts driver.StartOptions) error {
	d.options = opts
	if d.fail {
		return errors.New("repair failed")
	}
	return nil
}

func (d *preparingDriver) Start(ctx context.Context, id string) error {
	d.started = true
	return d.Driver.Start(ctx, id)
}

func TestPreparedResumeKeepsResourceOnFailureAndPersistsFreshInputs(t *testing.T) {
	svc, drv, st := testService(t)
	sb, err := svc.Create(context.Background(), CreateRequest{Env: map[string]string{"GRASP_RUNTIME_URL": "http://expired", "SANDBOX_INJECT": "http://expired/config", "SSH_KEY": "original-key"}})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	if err := svc.Stop(context.Background(), sb.ID); err != nil {
		t.Fatal(err)
	}
	prepared := &preparingDriver{Driver: drv, fail: true}
	svc.drv = prepared
	opts := driver.StartOptions{BootstrapScript: "#!/bin/bash\n", RuntimeURL: "http://fresh", RuntimeHeaders: "Authorization: Bearer fresh"}
	if err := svc.StartPrepared(context.Background(), sb.ID, opts); err == nil {
		t.Fatal("expected repair failure")
	}
	if prepared.started {
		t.Fatal("started before preparation succeeded")
	}
	if status, err := drv.Status(context.Background(), sb.ID); err != nil || status != driver.StatusStopped {
		t.Fatalf("failed repair lost resource: %s %v", status, err)
	}
	prepared.fail = false
	if err := svc.StartPrepared(context.Background(), sb.ID, opts); err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	got, err := st.Get(context.Background(), sb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != sb.ID || got.Env()["GRASP_RUNTIME_URL"] != opts.RuntimeURL || got.Env()["SANDBOX_INJECT_HEADERS"] != opts.RuntimeHeaders || got.Env()["SANDBOX_INJECT"] != "" || got.Env()["SSH_KEY"] != "original-key" {
		t.Fatalf("resume replaced sandbox or its grants: %+v", got)
	}
	if prepared.options != opts {
		t.Fatal("preparation inputs were dropped")
	}
}

func TestPreparedResumeRejectsNonBootstrapSandbox(t *testing.T) {
	svc, _, _ := testService(t)
	sb, err := svc.Create(context.Background(), CreateRequest{})
	if err != nil {
		t.Fatal(err)
	}
	waitSvcRunning(t, svc, sb.ID)
	if err := svc.StartPrepared(context.Background(), sb.ID, driver.StartOptions{RuntimeURL: "http://runtime"}); err == nil {
		t.Fatal("bootstrap repair accepted for an unrelated image")
	}
}
