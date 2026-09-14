package guild

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// GUILD.MD IS THE OPERATOR'S RUNBOOK, AND A FLAG IT NAMES THAT DOES NOT EXIST
// STOPS THE TOOL.
//
// This is internal/chat/docdrift_test.go's check, pointed at the other runbook,
// and it exists for the same recorded reason: Go's flag package does not ignore an
// unknown flag, it prints "flag provided but not defined" and exits 2. CHAT.md
// documented `--dry-run` for a while against a binary that defined only --enforce,
// so an operator following the notes and stating the safe mode explicitly — the
// careful thing to do — got exit 2 and no scanner. That is not a documentation
// blemish; it is a command that will not run, found by whoever is deploying.
//
// The direction is one-way on purpose: every flag the DOCUMENT names must exist.
// The reverse is a much weaker property, since not every flag needs prose.
//
// It scans BOTH declaration styles. internal/chat's version matches `flag.String(`,
// which kourtguildctl does not use — it builds a FlagSet per verb, so its flags are
// `fs.String(`. Missing that second form would make every documented flag look
// undefined, which is the false-positive direction and the one that gets a check
// deleted rather than fixed.
func TestEveryFlagGuildMdNamesActuallyExists(t *testing.T) {
	root := filepath.Join("..", "..")
	doc, err := os.ReadFile(filepath.Join(root, "GUILD.md"))
	if err != nil {
		t.Fatalf("GUILD.md is this feature's runbook and must be readable: %v", err)
	}

	defined := map[string]bool{}
	decl := regexp.MustCompile(`\b(?:flag|fs)\.[A-Za-z]+(?:Var)?\((?:&[A-Za-z_][A-Za-z0-9_.]*,\s*)?"([a-z0-9-]+)"`)
	cmds, err := filepath.Glob(filepath.Join(root, "cmd", "*", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) == 0 {
		t.Fatal("no command sources found; this test cannot verify anything")
	}
	for _, p := range cmds {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range decl.FindAllSubmatch(src, -1) {
			defined[string(m[1])] = true
		}
	}

	// GUARD THE FIXTURE'S OWN PRECONDITIONS. If the scan stops matching, every
	// documented flag looks missing — loud, but the opposite mistake is the
	// dangerous one, so the scan is pinned by names only it can supply.
	for _, must := range []string{"guild", "apply", "court", "client-id"} {
		if !defined[must] {
			t.Fatalf("the flag scan is broken: it did not find --%s, which "+
				"kourtguildctl certainly defines. Found %d: %v",
				must, len(defined), keys(defined))
		}
	}

	named := regexp.MustCompile("`(--[a-z0-9-]+)`").FindAllSubmatch(doc, -1)
	if len(named) == 0 {
		t.Fatal("GUILD.md names no flags in backticks at all; either the runbook lost " +
			"its command section or this pattern no longer matches how they are written")
	}
	seen, missing := map[string]bool{}, []string(nil)
	for _, m := range named {
		f := strings.TrimPrefix(string(m[1]), "--")
		if seen[f] {
			continue
		}
		seen[f] = true
		if !defined[f] {
			missing = append(missing, "--"+f)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("GUILD.md names %d flag(s) no binary defines: %v\n"+
			"Each is a documented invocation that exits 2. Add the flag or fix the prose.\n"+
			"Defined: %v", len(missing), missing, keys(defined))
	}
	t.Logf("%d distinct flags named in GUILD.md, all defined", len(seen))
}

// The same one-way check for the verbs, which is the other thing the runbook hands
// an operator to type. A wrong verb fails more gently than a wrong flag — the tool
// prints its usage — but the runbook is still wrong.
func TestEveryVerbGuildMdNamesIsDispatched(t *testing.T) {
	root := filepath.Join("..", "..")
	doc, err := os.ReadFile(filepath.Join(root, "GUILD.md"))
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(root, "cmd", "kourtguildctl", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	sw := regexp.MustCompile(`(?s)switch verb \{.*?\n\t\}`).Find(src)
	if sw == nil {
		t.Skip("kourtguildctl no longer dispatches through a switch on verb; re-derive this")
	}
	verbs := map[string]bool{}
	for _, m := range regexp.MustCompile(`case ((?:"[a-z-]+"(?:,\s*)?)+):`).FindAllSubmatch(sw, -1) {
		for _, q := range regexp.MustCompile(`"([a-z-]+)"`).FindAllSubmatch(m[1], -1) {
			verbs[string(q[1])] = true
		}
	}
	if len(verbs) < 4 {
		t.Fatalf("the verb scan found only %d (%v); it is broken", len(verbs), keys(verbs))
	}

	named := regexp.MustCompile(`kourtguildctl ([a-z-]+)`).FindAllSubmatch(doc, -1)
	if len(named) == 0 {
		t.Fatal("GUILD.md shows no kourtguildctl invocations at all")
	}
	seen, missing := map[string]bool{}, []string(nil)
	for _, m := range named {
		v := string(m[1])
		if seen[v] {
			continue
		}
		seen[v] = true
		if !verbs[v] {
			missing = append(missing, v)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("GUILD.md names %d verb(s) that are not dispatched: %v\nActual: %v",
			len(missing), missing, keys(verbs))
	}
}

// THE RUNBOOK MUST KEEP SAYING THE THREE THINGS THAT FAIL SILENTLY.
//
// Every other fact in GUILD.md announces itself when it is wrong. These three do
// not: a missing portal toggle, an Administrator grant, and the guild cap each
// produce a system that looks correct right up until it isn't. Prose is the only
// place they are recorded, so prose is what gets pinned.
func TestTheRunbookStillWarnsAboutTheSilentFailures(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "GUILD.md"))
	if err != nil {
		t.Fatal(err)
	}
	low := strings.ToLower(string(doc))
	for _, must := range []struct{ needle, why string }{
		{"require oauth2 code grant", "without this toggle the whole callback never fires and nothing errors"},
		{"public bot", "without it nobody but the app owner can add the bot"},
		{"100", "the unverified-bot guild cap is what bounds the whole design"},
		{"administrator", "the reason it is refused has to survive the next person's edit"},
		{"iscourtmod", "who may publish is the anti-front-running rule"},
	} {
		if !strings.Contains(low, must.needle) {
			t.Errorf("GUILD.md no longer mentions %q — %s", must.needle, must.why)
		}
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
