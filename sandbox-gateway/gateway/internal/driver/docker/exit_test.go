package docker

import (
	"context"
	"errors"
	"testing"
)

func TestLastExit(t *testing.T) {
	d := New(Options{})
	m := newMock()
	d.run = m.run

	m.on("inspect", `{"State":{"Status":"exited","OOMKilled":true,"ExitCode":137,"FinishedAt":"2026-10-07T22:31:04.5Z"},"RestartCount":2,"HostConfig":{"Memory":8589934592}}`, nil)
	e, err := d.LastExit(context.Background(), "abc")
	if err != nil || e == nil || !e.OOMKilled || e.Reason != "OOMKilled" || e.ExitCode != 137 || e.Restarts != 2 || e.MemoryMB != 8192 || e.At.IsZero() {
		t.Fatalf("e=%+v err=%v", e, err)
	}
	if got := m.lastCall(); got[len(got)-1] != "sbx-abc" {
		t.Fatalf("inspected %v", got)
	}

	m.on("inspect", `{"State":{"Status":"exited","ExitCode":1,"Error":"boom","FinishedAt":"2026-10-07T22:31:04Z"}}`, nil)
	if e, _ := d.LastExit(context.Background(), "abc"); e == nil || e.Reason != "Error" || e.Message != "boom" {
		t.Fatalf("e=%+v", e)
	}
	m.on("inspect", `{"State":{"Status":"exited","ExitCode":0,"FinishedAt":"2026-10-07T22:31:04Z"}}`, nil)
	if e, _ := d.LastExit(context.Background(), "abc"); e == nil || e.Reason != "Exited" {
		t.Fatalf("e=%+v", e)
	}

	m.on("inspect", `{"State":{"Status":"running","FinishedAt":"0001-01-01T00:00:00Z"}}`, nil)
	if e, err := d.LastExit(context.Background(), "abc"); e != nil || err != nil {
		t.Fatalf("never exited: e=%+v err=%v", e, err)
	}
	m.on("inspect", ``, errors.New("exit status 1: Error: No such container: sbx-abc"))
	if e, err := d.LastExit(context.Background(), "abc"); e != nil || err != nil {
		t.Fatalf("gone: e=%+v err=%v", e, err)
	}
	m.on("inspect", ``, errors.New("daemon down"))
	if _, err := d.LastExit(context.Background(), "abc"); err == nil {
		t.Fatal("want error")
	}
	m.on("inspect", `not json`, nil)
	if _, err := d.LastExit(context.Background(), "abc"); err == nil {
		t.Fatal("want parse error")
	}
}
