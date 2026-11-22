package updater

import (
	"strings"
	"testing"
)

// Security finding 7: secrets, profile paths and account names in the
// formats real tool output uses are redacted.
func TestSanitizeLogTextRedactsCommonSecretFormats(t *testing.T) {
	cases := map[string]string{
		"token=abc123secret":                         "abc123secret",
		"token: abc123secret":                        "abc123secret",
		`{"token":"abc123secret"}`:                   "abc123secret",
		`{"session_token": "abc123secret"}`:          "abc123secret",
		"apiKey=abc123secret":                        "abc123secret",
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9": "eyJhbGciOiJIUzI1NiJ9",
		`C:\Users\alice\AppData\Local\x`:             `\alice\`,
		`C:/Users/alice/AppData`:                     "/alice/",
		`\\?\C:\Users\alice\AppData`:                 `\alice\`,
		`\\fileserver\c$\Users\alice\Desktop`:        `\alice\`,
		"S-1-5-21-1111-2222-3333-1001":               "1111-2222",
	}
	for input, secret := range cases {
		got := sanitizeLogText("prefix " + input + " suffix")
		if strings.Contains(got, secret) {
			t.Errorf("sanitizeLogText(%q) = %q, still contains %q", input, got, secret)
		}
		if !strings.HasPrefix(got, "prefix ") || !strings.HasSuffix(got, " suffix") {
			t.Errorf("sanitizeLogText(%q) = %q, lost surrounding text", input, got)
		}
	}
	for _, keep := range []string{"Found 3 tokens in output", "winget upgrade --id Git.Git", "password reset not supported"} {
		if got := sanitizeLogText(keep); got != keep {
			t.Errorf("sanitizeLogText(%q) changed ordinary text to %q", keep, got)
		}
	}
}

func TestSanitizeLogTextRedactsCurrentAccountName(t *testing.T) {
	t.Setenv("USERDOMAIN", "CONTOSO")
	t.Setenv("USERNAME", "alice.example")
	registerCurrentAccountForLogRedaction()
	if got := sanitizeLogText(`owner CONTOSO\alice.example`); strings.Contains(got, `CONTOSO\alice.example`) {
		t.Fatalf("account name not redacted: %q", got)
	}
}
