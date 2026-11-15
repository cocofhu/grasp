package services

import (
	"testing"
)

func TestDefaultWorkflowEnvelopeHasDefaults(t *testing.T) {
	env, err := loadDefaultWorkflowEnvelope()
	if err != nil {
		t.Fatal(err)
	}
	if env.Defaults == nil {
		t.Fatal("template defaults missing")
	}
}
