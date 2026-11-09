package workspacesnapshot

import (
	"context"

	"backend/internal/workspacebinding"
)

// RootConsumer is used only with handlers/collectors that pin Binding.Acquire
// for their entire operation. Dynamic acquisition is the switch itself; there
// is no independent path cache or background root replacement.
func (c *Controller) RootConsumer(name string) workspacebinding.Consumer {
	return rootConsumer{controller: c, name: name}
}

type rootConsumer struct {
	controller *Controller
	name       string
}

func (c rootConsumer) Name() string { return c.name }
func (c rootConsumer) Prepare(ctx context.Context, _, _ string) (workspacebinding.Transition, error) {
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}
	if c.controller.Binding == nil || c.controller.Engine.binding != c.controller.Binding || (c.name != "capture" && c.name != "code") {
		return nil, workspacebinding.ErrUninstalled
	}
	return rootTransition{}, nil
}

type rootTransition struct{}

func (rootTransition) Apply(ctx context.Context) error  { return context.Cause(ctx) }
func (rootTransition) Verify(ctx context.Context) error { return context.Cause(ctx) }
func (rootTransition) Close() error                     { return nil }
