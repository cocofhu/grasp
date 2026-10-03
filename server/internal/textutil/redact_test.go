package textutil

import "testing"

func TestRedactSecrets(t *testing.T) {
	cases := map[string]string{
		"plain text":                              "plain text",
		"key sk-abcdefghijklmnopqrstu end":        "key **** end",
		"ghp_abcdefghijklmnopqrstuvwxyz":          "****",
		"Authorization: Bearer abcdefghijklmnopq": "Authorization: ****",
		"password=hunter22 ok":                    "password=**** ok",
		"token: short":                            "token: short",
	}
	for in, want := range cases {
		if got := RedactSecrets(in); got != want {
			t.Errorf("RedactSecrets(%q) = %q, want %q", in, got, want)
		}
	}
}
