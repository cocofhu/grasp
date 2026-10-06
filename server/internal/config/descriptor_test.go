package config

import (
	"os"
	"regexp"
	"testing"
)

func TestOptionDescriptorsCoverRuntimeEnvironment(t *testing.T) {
	known := map[string]bool{}
	for _, option := range OptionDescriptors() {
		if option.Env == "" || option.YAML == "" || option.Type == "" {
			t.Fatalf("incomplete descriptor: %#v", option)
		}
		if known[option.Env] {
			t.Fatalf("duplicate descriptor %s", option.Env)
		}
		known[option.Env] = true
	}

	source, err := os.ReadFile("config.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`env(?:Int)?\("([A-Z0-9_]+)"\)`)
	for _, match := range re.FindAllStringSubmatch(string(source), -1) {
		if !known[match[1]] {
			t.Errorf("runtime environment %s has no option descriptor", match[1])
		}
	}
}

func TestOptionDescriptorsUseGraspPrefix(t *testing.T) {
	for _, option := range OptionDescriptors() {
		if !regexp.MustCompile(`^GRASP_[A-Z0-9_]+$`).MatchString(option.Env) {
			t.Errorf("descriptor env %s is not GRASP_*", option.Env)
		}
	}
}
