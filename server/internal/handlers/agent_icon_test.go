package handlers

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/cocofhu/grasp/internal/services"
)

func TestAgentBodyRejectsEmojiAndBadIcon(t *testing.T) {
	cases := []string{
		`{"name":"a","emoji":"x"}`,
		`{"name":"a","icon":{"emoji":"x","color":"black"}}`,
		`{"name":"a","icon":{"kind":"emoji","iconId":"flask","color":"black"}}`,
		`{"name":"a","icon":{"iconId":"not-real","color":"black"}}`,
		`{"name":"a","icon":{"iconId":"flask","color":"teal"}}`,
		`{"name":"a","icon":{"iconId":"flask","color":"purple","svg":"<svg/>"}}`,
	}
	for _, raw := range cases {
		var b agentBody
		err := json.Unmarshal([]byte(raw), &b)
		if !errors.Is(err, services.ErrInvalidAgent) {
			t.Fatalf("raw %s err=%v", raw, err)
		}
	}
	var ok agentBody
	raw := `{"name":"a","inheritModel":false,"icon":{"iconId":"flask","color":"purple"}}`
	if err := json.Unmarshal([]byte(raw), &ok); err != nil {
		t.Fatal(err)
	}
	if ok.Icon == nil || ok.Icon.IconID != "flask" || ok.Icon.Color != "purple" {
		t.Fatalf("%+v", ok.Icon)
	}
	agent := ok.toAgent("renamed")
	if agent.Name != "renamed" || agent.Icon == nil || agent.Icon.IconID != "flask" || agent.Icon.Color != "purple" {
		t.Fatalf("%+v", agent.Icon)
	}
	var cleared agentBody
	if err := json.Unmarshal([]byte(`{"name":"a","icon":null}`), &cleared); err != nil {
		t.Fatal(err)
	}
	if cleared.toAgent("a").Icon != nil {
		t.Fatal("null icon was kept")
	}
}
