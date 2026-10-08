package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"sandbox-gateway/internal/driver"
)

var _ driver.ExitReporter = (*Driver)(nil)

type inspectExit struct {
	State struct {
		Status     string `json:"Status"`
		OOMKilled  bool   `json:"OOMKilled"`
		ExitCode   int    `json:"ExitCode"`
		Error      string `json:"Error"`
		FinishedAt string `json:"FinishedAt"`
	} `json:"State"`
	RestartCount int `json:"RestartCount"`
	HostConfig   struct {
		Memory int64 `json:"Memory"`
	} `json:"HostConfig"`
}

// LastExit reads the container's last exit from docker inspect. A container
// that has never stopped reports a zero FinishedAt and yields nil.
func (d *Driver) LastExit(ctx context.Context, id string) (*driver.ExitInfo, error) {
	out, err := d.run(ctx, 10*time.Second, "inspect", "--format",
		"{{json .}}", d.containerName(id))
	if err != nil {
		if isNoSuchContainer(err) {
			return nil, nil
		}
		return nil, err
	}
	var in inspectExit
	if err := json.Unmarshal([]byte(out), &in); err != nil {
		return nil, fmt.Errorf("parse docker inspect: %w", err)
	}
	at, _ := time.Parse(time.RFC3339Nano, in.State.FinishedAt)
	if at.Year() <= 1 {
		return nil, nil
	}
	reason := "Exited"
	switch {
	case in.State.OOMKilled:
		reason = "OOMKilled"
	case in.State.ExitCode != 0:
		reason = "Error"
	}
	return &driver.ExitInfo{
		Reason:    reason,
		ExitCode:  in.State.ExitCode,
		OOMKilled: in.State.OOMKilled,
		Message:   in.State.Error,
		Restarts:  in.RestartCount,
		MemoryMB:  in.HostConfig.Memory / (1 << 20),
		At:        at,
	}, nil
}
