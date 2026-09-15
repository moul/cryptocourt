package binding

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// THE COURT IS WHAT AUTHORISES A BINDING. THE GUILD IS ONLY ITS NAME.
//
// This package has had the same bug twice, and both times every individual check
// passed — only the combination was wrong:
//
//	Record  upserted on (chain, guild_id) and moved `court` unconditionally, so a
//	        guild already LISTED for one court could be re-pointed at another by
//	        anybody who could complete an OAuth round trip.
//	List    delisted on (chain, court) and listed on (chain, guild_id), so a
//	        moderator of court B could publish a guild bound to court A, putting
//	        B's signer on A's row.
//
// Neither was visible to a test of either function. Both were a statement keyed
// on the GUILD when the COURT is what grants the right to touch the row. A third
// instance is likelier than not, so this refuses one at compile-of-the-suite time
// rather than waiting for somebody to think of the scenario.
//
// WHAT THIS DOES NOT CHECK: that the court passed in is the RIGHT one. A caller
// that threads an attacker-supplied court through to the store satisfies every
// rule here. This holds the statements to a shape; the handlers' own tests hold
// them to the values.
// rowDriven names the Store methods whose guild id comes from a row this package
// just read back, rather than from a caller. They are the legitimate exemption
// and the ONLY one, and the list is shared with the signature check below so the
// two cannot disagree about which is which.
var rowDriven = map[string]string{
	"SetState": "moves a row the heartbeat or the sweep just read from this store",
	"Degrade":  "same; the heartbeat hands back what Watched gave it",
	"Restore":  "same",
	"Touch":    "same, and it writes only a timestamp",
	"Record": "the one place `court` is the value being WRITTEN rather than the " +
		"key, guarded by state = 'pending' so a listed row cannot be re-aimed",
}

// methods splits store.go into (name, body) for each Store method.
func methods(t *testing.T) map[string]string {
	t.Helper()
	src, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	re := regexp.MustCompile(`(?s)func \(s \*Store\) ([A-Z]\w*)\(.*?\n\}`)
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		out[m[1]] = m[0]
	}
	if len(out) < 8 {
		t.Fatalf("the method scan found %d methods; it is broken, and a broken scan "+
			"reports agreement over nothing", len(out))
	}
	return out
}

func TestEveryBindingWriteIsKeyedOnTheCourt(t *testing.T) {
	var checked, exempted int
	for name, body := range methods(t) {
		stmts := regexp.MustCompile("(?s)`UPDATE guild_bindings.*?`").FindAllString(body, -1)
		if len(stmts) == 0 {
			continue
		}
		if why, ok := rowDriven[name]; ok {
			exempted++
			if why == "" {
				t.Errorf("Store.%s is exempted with no reason", name)
			}
			continue
		}
		checked++
		for _, st := range stmts {
			if !strings.Contains(st, "court = ?") {
				t.Errorf("Store.%s writes guild_bindings without filtering on the court:\n  %s\n\n"+
					"The court is what authorises touching a binding; the guild is only "+
					"its name. This package has shipped that mistake twice. If this is "+
					"genuinely row-driven, add %s to rowDriven with the reason.",
					name, strings.Join(strings.Fields(st), " "), name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no caller-driven write was checked; this test measured nothing")
	}
	t.Logf("%d caller-driven method(s) court-keyed, %d row-driven exempted with a reason",
		checked, exempted)
}

// The exemption list must not rot into names nobody can find.
func TestTheExemptionsStillNameRealMethods(t *testing.T) {
	m := methods(t)
	for name, why := range rowDriven {
		if _, ok := m[name]; !ok {
			t.Errorf("rowDriven names Store.%s (%q), which no longer exists; an "+
				"exemption that matches nothing excuses nothing and hides the next "+
				"statement that needs excusing", name, why)
		}
	}
}

// AND THE PUBLIC SHAPE, so a court-keyed statement cannot be reached by a method
// that never takes a court. Both bugs were in functions whose signature already
// carried one; a function that does not is the shape to notice before it grows a
// caller, not after.
func TestMutatorsThatTouchBindingsTakeACourt(t *testing.T) {
	src, err := os.ReadFile("store.go")
	if err != nil {
		t.Fatal(err)
	}
	sigs := regexp.MustCompile(`func \(s \*Store\) ([A-Z]\w*)\(ctx context\.Context, ([^)]*)\)`).
		FindAllStringSubmatch(string(src), -1)
	if len(sigs) < 6 {
		t.Fatalf("the signature scan found %d methods; it is broken", len(sigs))
	}
	var checked int
	for _, m := range sigs {
		name, params := m[1], m[2]
		if _, exempt := rowDriven[name]; !strings.Contains(params, "guildID") || exempt {
			continue
		}
		checked++
		if !strings.Contains(params, "court") {
			t.Errorf("Store.%s takes a guild id but no court. Either it is row-driven "+
				"— add it to rowDriven above — or it is the third instance of this "+
				"package's recurring bug.", name)
		}
	}
	if checked == 0 {
		t.Fatal("no caller-driven mutator was checked; this test measured nothing")
	}
}
