package services

import (
	"testing"
)

func TestDefaultWorkflowEnvelopeInheritsWorkspace(t *testing.T) {
	env, err := loadDefaultWorkflowEnvelope()
	if err != nil {
		t.Fatal(err)
	}
	if d := env.Defaults; d == nil || !d.InheritWorkspace {
		t.Fatalf("template defaults = %+v, want inheritWorkspace", d)
	}
}
