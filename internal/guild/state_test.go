package guild

import (
	"strings"
	"testing"
)

// A PARSER THAT REFUSES EVERYTHING PASSES A TABLE OF REFUSALS.
//
// internal/chat/sanitize_test.go opens with that rule and it is the right one
// here too: every rejection below is paired with the ordinary input it must not
// reject, because the failure mode of a state grammar is not "an attacker gets
// through", it is "a real moderator's bind errors at the redirect with nothing to
// retry, and nobody can reproduce it".

const goodNonce = "0123456789abcdef0123456789abcdef" // 16 bytes

func TestStateRoundTrips(t *testing.T) {
	in := State{Chain: "kourt-1", Court: "meta", Nonce: goodNonce}
	s, err := FormatState(in)
	if err != nil {
		t.Fatalf("formatting an ordinary state failed: %v", err)
	}
	if s != "v1:kourt-1:meta:"+goodNonce {
		t.Errorf("format produced %q", s)
	}
	out, err := ParseState(s)
	if err != nil {
		t.Fatalf("parsing back what we wrote failed: %v", err)
	}
	if out != in {
		t.Errorf("round trip changed the value: %+v -> %+v", in, out)
	}
}

func TestParseStateAcceptsTheOrdinaryAndRefusesTheRest(t *testing.T) {
	ok := []struct{ name, in string }{
		{"the live chain", "v1:kourt-1:meta:" + goodNonce},
		{"a local chain", "v1:dev:covid:" + goodNonce},
		{"the longest legal slug", "v1:dev:abcdefghijk:" + goodNonce},
		{"a one-character slug", "v1:dev:a:" + goodNonce},
		{"digits in a slug", "v1:dev:court2:" + goodNonce},
		{"a longer nonce", "v1:dev:meta:" + goodNonce + goodNonce},
	}
	for _, c := range ok {
		if _, err := ParseState(c.in); err != nil {
			t.Errorf("%s: rejected %q: %v", c.name, c.in, err)
		}
	}

	bad := []struct{ name, in string }{
		{"no version", "kourt-1:meta:" + goodNonce},
		{"wrong version", "v2:kourt-1:meta:" + goodNonce},
		{"too few parts", "v1:meta:" + goodNonce},
		{"too many parts", "v1:dev:meta:" + goodNonce + ":extra"},
		{"empty court", "v1:dev::" + goodNonce},
		{"empty chain", "v1::meta:" + goodNonce},
		{"empty nonce", "v1:dev:meta:"},

		// The slug rules are the chain's, and each of these is a real slug
		// somewhere else in software and not here.
		{"hyphen in slug", "v1:dev:my-court:" + goodNonce},
		{"uppercase slug", "v1:dev:META:" + goodNonce},
		{"slug too long", "v1:dev:abcdefghijkl:" + goodNonce},
		{"underscore in slug", "v1:dev:my_court:" + goodNonce},
		{"dot in slug", "v1:dev:a.b:" + goodNonce},

		// A nonce is entropy, so shape alone is not enough.
		{"nonce too short", "v1:dev:meta:0123456789abcdef"},
		{"nonce not hex", "v1:dev:meta:" + strings.Repeat("z", 32)},
		{"nonce uppercase hex", "v1:dev:meta:" + strings.ToUpper(goodNonce)},
		{"nonce odd length", "v1:dev:meta:" + goodNonce + "a"},
	}
	for _, c := range bad {
		if _, err := ParseState(c.in); err == nil {
			t.Errorf("%s: accepted %q", c.name, c.in)
		}
	}
}

// FormatState validates its own caller because the alternative is minting a link
// whose state cannot be read on the way back — a failure that lands on a real
// moderator, at a redirect, with nothing to retry.
func TestFormatStateRefusesWhatParseWouldRefuse(t *testing.T) {
	bad := []struct {
		name string
		in   State
	}{
		{"hyphenated slug", State{"dev", "my-court", goodNonce}},
		{"empty slug", State{"dev", "", goodNonce}},
		{"empty chain", State{"", "meta", goodNonce}},
		{"short nonce", State{"dev", "meta", "abcd"}},
		{"no nonce at all", State{"dev", "meta", ""}},
	}
	for _, c := range bad {
		if s, err := FormatState(c.in); err == nil {
			t.Errorf("%s: formatted %q", c.name, s)
		}
	}
	if _, err := FormatState(State{"kourt-1", "meta", goodNonce}); err != nil {
		t.Errorf("refused an ordinary state: %v", err)
	}
}

// A colon in any part would let one field impersonate the next. The charset rules
// make that unreachable, so this asserts the property rather than the rule: no
// accepted state can round-trip into different fields.
func TestNoPartCanSmuggleASeparator(t *testing.T) {
	if _, err := FormatState(State{"dev", "me:ta", goodNonce}); err == nil {
		t.Error("a court slug containing a colon was accepted")
	}
	if _, err := FormatState(State{"de:v", "meta", goodNonce}); err == nil {
		t.Error("a chain id containing a colon was accepted")
	}
}

// THE CHALLENGE IS THE SIGNATURE'S WHOLE MEANING.
//
// An earlier draft of this design said only "the moderator signs a challenge".
// A challenge missing the guild id is a reusable warrant: one signature, pasted
// into a support channel or screenshotted, lists any server its holder likes for
// that court. So the test is not that the function returns a string — it is that
// changing any one of the three bound values changes the text.
func TestTheChallengeBindsCourtGuildAndNonce(t *testing.T) {
	base := State{Chain: "kourt-1", Court: "meta", Nonce: goodNonce}
	const gid = "1478455953715236886"

	got, err := ChallengeText(base, gid)
	if err != nil {
		t.Fatalf("an ordinary challenge failed: %v", err)
	}
	for _, must := range []string{gid, "meta", "kourt-1", goodNonce} {
		if !strings.Contains(got, must) {
			t.Errorf("the challenge does not name %q:\n%s", must, got)
		}
	}

	other := State{Chain: "kourt-1", Court: "covid", Nonce: goodNonce}
	if o, _ := ChallengeText(other, gid); o == got {
		t.Error("two different courts produce the same challenge")
	}
	if o, _ := ChallengeText(base, "1478455953715236887"); o == got {
		t.Error("two different guilds produce the same challenge")
	}
	nonced := base
	nonced.Nonce = strings.Repeat("ab", 16)
	if o, _ := ChallengeText(nonced, gid); o == got {
		t.Error("two different nonces produce the same challenge")
	}
}

func TestTheChallengeRefusesAThingThatIsNotASnowflake(t *testing.T) {
	s := State{Chain: "dev", Court: "meta", Nonce: goodNonce}
	for _, bad := range []string{"", "abc", "12345", "14784559537152368861234", "147845595371523688a"} {
		if _, err := ChallengeText(s, bad); err == nil {
			t.Errorf("accepted %q as a guild id", bad)
		}
	}
	if _, err := ChallengeText(s, "1478455953715236886"); err != nil {
		t.Errorf("refused a real snowflake: %v", err)
	}
}

// The slug rule here is deliberately narrower than the two already in the tree.
// If somebody widens it to match them, this fails and says why.
func TestTheSlugRuleIsTheChainsAndNotTheChatServices(t *testing.T) {
	if courtRe.MatchString("my-court") {
		t.Error("the slug rule accepts a hyphen. The chain does not " +
			"(realm/r/kourtv2/court.gno mustSlug), because a court's coin symbol is " +
			"its upper-cased slug and KOURT:MY-COURT reads the hyphen as a minus. " +
			"internal/chat and internal/archive are wider on purpose; this one signs.")
	}
	if courtRe.MatchString(strings.Repeat("a", 12)) {
		t.Error("the slug rule accepts 12 characters; the chain's limit is 11")
	}
}
