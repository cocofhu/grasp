package modelrouting

import "testing"

func TestOpenCodeVariantCatalogMatchesOpenCode(t *testing.T) {
	if len(openCodeVariantCatalog) != 133 {
		t.Fatalf("catalog has %d models", len(openCodeVariantCatalog))
	}
	with := 0
	for _, entry := range openCodeVariantCatalog {
		if len(entry.Variants) > 0 {
			with++
		}
	}
	if with != 90 || len(openCodeVariantCatalog)-with != 43 {
		t.Fatalf("variants: %d with, %d without", with, len(openCodeVariantCatalog)-with)
	}
	if got, zen := OpenCodePlatformVariants("opencode-go/gpt-5.5"), OpenCodePlatformVariants("opencode/gpt-5.5"); len(got) != len(zen) || len(got) == 0 || got[0] != zen[0] {
		t.Fatalf("go prefix missed the shared catalog: %v vs %v", got, zen)
	}
	if OpenCodePlatformVariants("opencode-go/mimo-v2.5") != nil || OpenCodeCatalogID("opencode-go/gpt-5.5#high") != "gpt-5.5" {
		t.Fatal("unlisted go model or variant suffix changed the catalog lookup")
	}
	if got := SessionEffortChoices(OpenCodePlatformVariants("opencode/gpt-5.5"), "turbo"); len(got) != 0 {
		t.Fatalf("custom level stayed on the chat menu: %v", got)
	}
	if got := SessionEffortChoices(OpenCodePlatformVariants("opencode/gpt-5.5"), "high"); len(got) != 5 {
		t.Fatalf("documented level hid the chat menu: %v", got)
	}
	if got := WithSavedEffort(OpenCodePlatformVariants("opencode/gpt-5.5"), "turbo"); len(got) != 6 || got[5] != "turbo" {
		t.Fatalf("custom variant was not appended: %v", got)
	}
	if got := WithSavedEffort(OpenCodePlatformVariants("opencode/gpt-5.5"), "high"); len(got) != 5 {
		t.Fatalf("catalog variant was duplicated: %v", got)
	}
	if WithSavedEffort(nil, "bad value") != nil || !ValidEffortToken("thinking") || ValidEffortToken("") {
		t.Fatal("custom token rules changed")
	}
}
