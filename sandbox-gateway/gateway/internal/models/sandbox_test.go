package models

import "testing"

func TestSetEnvRoundtrip(t *testing.T) {
	s := &Sandbox{}
	if len(s.Env()) != 0 {
		t.Fatalf("empty: %v", s.Env())
	}
	s.SetEnv(nil)
	if s.EnvJSON != "" {
		t.Fatalf("nil encode: %q", s.EnvJSON)
	}
	s.SetEnv(map[string]string{})
	if s.EnvJSON != "" {
		t.Fatalf("empty encode: %q", s.EnvJSON)
	}
	s.SetEnv(map[string]string{"A": "1", "B": "two"})
	got := s.Env()
	if got["A"] != "1" || got["B"] != "two" {
		t.Fatalf("%v", got)
	}
}

func TestSetEndpointsRoundtrip(t *testing.T) {
	s := &Sandbox{}
	if len(s.Endpoints()) != 0 {
		t.Fatal("empty")
	}
	s.SetEndpoints(nil)
	if s.EndpointsJSON != "" {
		t.Fatal("nil")
	}
	s.SetEndpoints(map[int]string{8765: "10.0.0.1:8765", 22: "10.0.0.1:22"})
	got := s.Endpoints()
	if got[8765] != "10.0.0.1:8765" || got[22] != "10.0.0.1:22" {
		t.Fatalf("%v", got)
	}
}

func TestSetLabelsRoundtrip(t *testing.T) {
	s := &Sandbox{}
	s.SetLabels(map[string]string{"team": "ops", "env": "dev"})
	got := s.Labels()
	if got["team"] != "ops" || got["env"] != "dev" {
		t.Fatalf("%v", got)
	}
}

func TestBadJSONDecodeEmpty(t *testing.T) {
	s := &Sandbox{
		EnvJSON:       "{not-json",
		EndpointsJSON: "[]",
		LabelsJSON:    "{",
	}
	if len(s.Env()) != 0 {
		t.Fatalf("env: %v", s.Env())
	}
	// EndpointsJSON that is valid JSON but not object → decodeIntMap returns empty on type mismatch...
	// actually Unmarshal into map fails for array → empty
	if len(s.Endpoints()) != 0 {
		t.Fatalf("endpoints: %v", s.Endpoints())
	}
	if len(s.Labels()) != 0 {
		t.Fatalf("labels: %v", s.Labels())
	}

	s.EndpointsJSON = `{"not-int":"x","22":"h:22"}`
	eps := s.Endpoints()
	if eps[22] != "h:22" {
		t.Fatalf("%v", eps)
	}
	if _, ok := eps[0]; ok {
		t.Fatal("non-int key should be skipped")
	}
}

func TestItoaAtoi(t *testing.T) {
	if itoa(42) != "42" {
		t.Fatal(itoa(42))
	}
	n, ok := atoi("99")
	if !ok || n != 99 {
		t.Fatal(n, ok)
	}
	if _, ok := atoi("x"); ok {
		t.Fatal("expected fail")
	}
}
