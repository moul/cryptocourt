package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func source(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// THE USAGE TEXT AND THE DISPATCH ARE TWO LISTS OF THE SAME THING.
//
// cmd/kourtchatctl/main_test.go reads its own source for exactly this reason, and
// the failure it guards against is the quiet half: a verb that is dispatched but
// undocumented is merely undiscoverable, while a verb the usage advertises and the
// switch does not handle sends an operator to "unknown verb" for something the
// tool told them to type.
func TestTheUsageAndTheDispatchAgree(t *testing.T) {
	src := source(t)

	sw := regexp.MustCompile(`(?s)switch verb \{.*?\n\t\}`).Find([]byte(src))
	if sw == nil {
		t.Fatal("no dispatch switch found; this test cannot verify anything")
	}
	dispatched := map[string]bool{}
	for _, m := range regexp.MustCompile(`case ((?:"[a-z-]+"(?:,\s*)?)+):`).FindAllSubmatch(sw, -1) {
		for _, q := range regexp.MustCompile(`"([a-z-]+)"`).FindAllSubmatch(m[1], -1) {
			dispatched[string(q[1])] = true
		}
	}
	// A census floor, so a scan that has stopped matching fails instead of
	// reporting agreement over nothing.
	if len(dispatched) < 4 {
		t.Fatalf("the dispatch scan found %d verb(s) (%v); it is broken",
			len(dispatched), sorted(dispatched))
	}

	usage := regexp.MustCompile(`(?s)func usage\(\) \{.*?\n\}`).FindString(src)
	if usage == "" {
		t.Fatal("no usage() found")
	}
	advertised := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^  ([a-z-]+)\s{2,}`).FindAllStringSubmatch(usage, -1) {
		advertised[m[1]] = true
	}
	if len(advertised) == 0 {
		t.Fatal("usage() advertises no verbs, or no longer lays them out two-space indented")
	}

	for v := range advertised {
		if !dispatched[v] {
			t.Errorf("usage advertises %q, which the switch does not handle", v)
		}
	}
	// The help family is dispatched and deliberately not advertised: `help`,
	// `-h` and `--help` all reach the same case, and listing the usage text
	// inside the usage text is noise. Everything else must appear.
	for v := range dispatched {
		if v == "help" || strings.HasPrefix(v, "-") {
			continue
		}
		if !advertised[v] {
			t.Errorf("%q is dispatched but usage never mentions it", v)
		}
	}
}

// THE TOKEN IS NEVER A FLAG, and that is a rule worth a test rather than a comment.
//
// A flag puts a credential that can ban people in every process listing on the box.
// The faucet's mnemonic went to a systemd credential for the same reason. Somebody
// adding --token for convenience during debugging is the realistic way this breaks,
// and it would work perfectly, which is why nothing else would catch it.
func TestTheBotTokenIsNotAFlag(t *testing.T) {
	src := source(t)
	decl := regexp.MustCompile(`\b(?:flag|fs)\.[A-Za-z]+(?:Var)?\((?:&[^,]+,\s*)?"([a-z0-9-]+)"`)
	found := decl.FindAllStringSubmatch(src, -1)
	if len(found) < 5 {
		t.Fatalf("the flag scan found %d flags; it is broken", len(found))
	}
	for _, m := range found {
		n := m[1]
		if strings.Contains(n, "token") || strings.Contains(n, "secret") ||
			strings.Contains(n, "password") {
			t.Errorf("--%s is a flag; credentials go through the environment "+
				"(%s), never the process table", n, tokenEnv)
		}
	}
	if !strings.Contains(src, "os.Getenv(tokenEnv)") {
		t.Error("the token is no longer read from the environment; if it moved, " +
			"this test needs to know where to")
	}
}

// DRY RUN IS THE DEFAULT, following kourtmod. build-template edits a live guild
// and mints the template every court server afterwards is copied from, so the safe
// mode has to be the one you get by forgetting a flag.
func TestApplyIsOptInAndContradictionsAreRefused(t *testing.T) {
	if err := cmdBuildTemplate([]string{"--guild", "1478455953715236886"}); err != nil {
		t.Errorf("a plain dry run failed: %v", err)
	}
	if err := cmdBuildTemplate([]string{"--guild", "1478455953715236886", "--dry-run", "--apply"}); err == nil {
		t.Error("--dry-run --apply together was accepted; one of them is a lie")
	}
	if err := cmdBuildTemplate(nil); err == nil {
		t.Error("build-template with no --guild was accepted")
	}
}

// --apply with no token must fail BEFORE anything is sent. The ordering is the
// point: a half-applied layout on a live guild is worse than a refusal, because
// the next run creates the roles a second time.
func TestApplyWithoutATokenRefusesBeforeSendingAnything(t *testing.T) {
	old, had := os.LookupEnv(tokenEnv)
	os.Unsetenv(tokenEnv)
	defer func() {
		if had {
			os.Setenv(tokenEnv, old)
		}
	}()

	err := cmdBuildTemplate([]string{"--guild", "1478455953715236886", "--apply"})
	if err == nil {
		t.Fatal("--apply with no token was accepted")
	}
	if !strings.Contains(err.Error(), tokenEnv) {
		t.Errorf("the error does not say where the token comes from: %v", err)
	}
}

func TestLinksNeedsEnoughToBuildALink(t *testing.T) {
	bad := [][]string{
		nil,
		{"--court", "meta"},
		{"--court", "meta", "--client-id", "123"},
		{"--client-id", "123", "--redirect", "https://kourt.xyz/x"},
	}
	for _, argv := range bad {
		if err := cmdLinks(argv); err == nil {
			t.Errorf("links %v was accepted", argv)
		}
	}
	if err := cmdLinks([]string{
		"--court", "meta", "--client-id", "123",
		"--redirect", "https://kourt.xyz/api/guild/bound",
	}); err != nil {
		t.Errorf("an ordinary links call failed: %v", err)
	}
}

// links must refuse a court the chain could not have, rather than printing a link
// whose state cannot be parsed on the way back.
func TestLinksRefusesACourtTheChainCouldNotHave(t *testing.T) {
	err := cmdLinks([]string{
		"--court", "my-court", "--client-id", "123",
		"--redirect", "https://kourt.xyz/api/guild/bound",
	})
	if err == nil {
		t.Error("a hyphenated slug produced a link; the chain's rule is [a-z0-9]{1,11}")
	}
}

// The two read-only verbs must stay read-only: they are what somebody runs to
// review the layout before touching a guild.
func TestPlanAndPermsTouchNothing(t *testing.T) {
	if err := cmdPlan(); err != nil {
		t.Errorf("plan: %v", err)
	}
	if err := cmdPerms(); err != nil {
		t.Errorf("perms: %v", err)
	}
}

func sorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
