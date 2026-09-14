package guild

import (
	"net/url"
	"strings"
	"testing"
)

const (
	testClientID = "1234567890123456789"
	testRedirect = "https://kourt.xyz/api/guild/bound"
)

func mustAuthorize(t *testing.T) url.Values {
	t.Helper()
	raw, err := AuthorizeURL(testClientID, testRedirect,
		State{Chain: "kourt-1", Court: "meta", Nonce: goodNonce})
	if err != nil {
		t.Fatalf("building an ordinary authorize url failed: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("we produced an unparseable url: %v", err)
	}
	return u.Query()
}

// EVERY ONE OF THESE PARAMETERS IS LOAD-BEARING and three of them fail SILENTLY
// when absent, which is why they are asserted individually rather than by
// comparing the whole string.
//
//	response_type=code  — without it Discord runs the plain invite flow: the bot
//	                      joins, the browser never returns, no code is issued, and
//	                      the callback simply never fires. Nothing errors.
//	redirect_uri        — same failure.
//	state               — the only thing that survives the round trip saying which
//	                      court this was for.
func TestTheAuthorizeUrlCarriesEveryParameterTheFlowNeeds(t *testing.T) {
	q := mustAuthorize(t)
	want := map[string]string{
		"client_id":        testClientID,
		"scope":            "bot applications.commands",
		"permissions":      RequiredBits().String(),
		"response_type":    "code",
		"redirect_uri":     testRedirect,
		"state":            "v1:kourt-1:meta:" + goodNonce,
		"integration_type": "0",
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if len(q) != len(want) {
		t.Errorf("the url carries %d parameters, expected %d: %v", len(q), len(want), q)
	}
}

// The permission integer must come FROM perms.go, not be restated here. A literal
// in urls.go would be a second definition of the set — the exact drift this
// package's shape exists to prevent — and it would look correct on the day it was
// written.
func TestThePermissionsParameterIsDerivedFromTheDeclaredSet(t *testing.T) {
	q := mustAuthorize(t)
	if q.Get("permissions") != RequiredBits().String() {
		t.Fatal("the authorize url's permissions do not match RequiredBits()")
	}
	// And prove the link actually moves when the set does, rather than both
	// sides happening to agree on a constant.
	if !strings.Contains(q.Get("permissions"), "1426734574711") {
		t.Errorf("permissions = %q; if the set changed deliberately, perms_test.go "+
			"should have failed first", q.Get("permissions"))
	}
}

// MESSAGE_CONTENT is a privileged intent, not a scope, so it cannot appear here —
// but the scope string is where somebody would try to add it, and a bot reading
// message content across thousands of stranger-owned guilds is the thing this
// design most wants to avoid.
func TestTheScopesAreTheTwoWeIntendAndNoMore(t *testing.T) {
	q := mustAuthorize(t)
	got := strings.Fields(q.Get("scope"))
	if len(got) != 2 || got[0] != "bot" || got[1] != "applications.commands" {
		t.Errorf("scopes = %v, want [bot applications.commands]", got)
	}
	for _, s := range got {
		if strings.Contains(strings.ToLower(s), "message") {
			t.Errorf("scope %q looks like message access; that is a deliberate no", s)
		}
	}
}

func TestTheAuthorizeUrlRefusesWhatItCannotSafelyBuild(t *testing.T) {
	good := State{Chain: "kourt-1", Court: "meta", Nonce: goodNonce}
	bad := []struct {
		name               string
		clientID, redirect string
		state              State
	}{
		{"no client id", "", testRedirect, good},
		{"blank client id", "   ", testRedirect, good},
		{"no redirect", testClientID, "", good},
		{"a plain-http redirect", testClientID, "http://kourt.xyz/api/guild/bound", good},
		{"a redirect that is not a url", testClientID, "://nope", good},
		{"a state naming an impossible court", testClientID, testRedirect,
			State{Chain: "dev", Court: "my-court", Nonce: goodNonce}},
		{"a state with no nonce", testClientID, testRedirect,
			State{Chain: "dev", Court: "meta", Nonce: ""}},
	}
	for _, c := range bad {
		if got, err := AuthorizeURL(c.clientID, c.redirect, c.state); err == nil {
			t.Errorf("%s: built %q", c.name, got)
		}
	}
	// The pairing that keeps the table honest.
	if _, err := AuthorizeURL(testClientID, testRedirect, good); err != nil {
		t.Errorf("refused an ordinary call: %v", err)
	}
}

// http is refused even though Discord allows it for localhost: the code in that
// redirect is a bearer value, and this function cannot tell a laptop from a link
// about to be published on a court page.
func TestAPlainHttpRedirectIsRefusedEvenForLocalhost(t *testing.T) {
	if _, err := AuthorizeURL(testClientID, "http://127.0.0.1:8788/api/guild/bound",
		State{Chain: "dev", Court: "meta", Nonce: goodNonce}); err == nil {
		t.Error("a plaintext localhost redirect was accepted")
	}
}

func TestTheTemplateLinkIsDiscordsShortlink(t *testing.T) {
	got, err := TemplateURL("abcDEF123")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://discord.new/abcDEF123" {
		t.Errorf("template url = %q", got)
	}
}

// The code is concatenated onto a host and handed to a reader to click, so the
// charset matters more than the length. Discord does not document either, which
// is why this is loose on shape and strict on what could escape a path.
func TestTheTemplateCodeCannotEscapeTheUrl(t *testing.T) {
	for _, bad := range []string{
		"", "a/b", "a?b", "a#b", "../x", "a b", "a%2f", strings.Repeat("a", 65),
	} {
		if got, err := TemplateURL(bad); err == nil {
			t.Errorf("accepted %q, producing %q", bad, got)
		}
	}
	for _, ok := range []string{"a", "AbC123", strings.Repeat("z", 64)} {
		if _, err := TemplateURL(ok); err != nil {
			t.Errorf("refused %q: %v", ok, err)
		}
	}
}
