package guild

import (
	"sort"
	"strings"
	"testing"
)

// THE INTEGER AND THE LIST ARE PINNED SEPARATELY, and that is the whole point of
// this file.
//
// A check on the total alone stays green while two bits swap — drop MANAGE_GUILD,
// add ADMINISTRATOR, and a sum can come out anywhere, including somewhere that
// looks familiar. A check on the list alone misses a shift typed as 1<<41. Both,
// and the pair cannot drift quietly.
//
// The number below was computed independently of the code under test, from
// Discord's published bit positions.
func TestTheRequiredSetIsExactlyWhatWeThinkItIs(t *testing.T) {
	const want = 1426734574711 // 0x14c3001ec77
	if got := RequiredBits(); uint64(got) != want {
		t.Errorf("RequiredBits() = %d, want %d\n"+
			"A changed total means a permission was added or removed. If that was "+
			"deliberate, the consent screen a stranger sees just changed too — say so "+
			"in the commit, then update this number.", uint64(got), want)
	}

	wantNames := []string{
		"ADD_REACTIONS", "ATTACH_FILES", "BAN_MEMBERS", "CREATE_INSTANT_INVITE",
		"CREATE_PUBLIC_THREADS", "EMBED_LINKS", "KICK_MEMBERS", "MANAGE_CHANNELS",
		"MANAGE_GUILD", "MANAGE_MESSAGES", "MANAGE_ROLES", "MANAGE_THREADS",
		"MANAGE_WEBHOOKS", "MODERATE_MEMBERS", "READ_MESSAGE_HISTORY",
		"SEND_MESSAGES", "SEND_MESSAGES_IN_THREADS", "VIEW_CHANNEL",
	}
	var got []string
	for _, n := range Required {
		got = append(got, n.Name)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(wantNames, ",") {
		t.Errorf("the required set changed\n got: %v\nwant: %v", got, wantNames)
	}
}

// ADMINISTRATOR is 1<<3 and must never appear. It is refused for a stated reason
// — it collapses "has the bot been quietly demoted" into a boolean that cannot
// answer the question — and a reason that is only in a comment is one somebody
// reverses without noticing.
func TestAdministratorIsNeverRequested(t *testing.T) {
	const administrator = Permission(1 << 3)
	if RequiredBits()&administrator != 0 {
		t.Fatal("ADMINISTRATOR is in the required set. It must not be: the whole " +
			"delisting design rests on being able to diff granted powers against " +
			"expected ones, and a wildcard makes that diff meaningless.")
	}
}

// Every bit is declared once. A duplicate is invisible in the total — OR is
// idempotent — so nothing else in this file would catch it.
func TestNoPermissionIsListedTwice(t *testing.T) {
	seen := map[Permission]string{}
	for _, n := range Required {
		if prev, dup := seen[n.Bit]; dup {
			t.Errorf("bit %s appears as both %q and %q", n.Bit, prev, n.Name)
		}
		seen[n.Bit] = n.Name
	}
}

// Every entry carries a justification, because this set is shown to strangers.
func TestEveryPermissionSaysWhyItIsNeeded(t *testing.T) {
	for _, n := range Required {
		if strings.TrimSpace(n.Why) == "" {
			t.Errorf("%s has no Why", n.Name)
		}
	}
	if len(Required) < 10 {
		t.Fatalf("only %d permissions declared; this scan has stopped measuring "+
			"anything real", len(Required))
	}
}

// Missing is what the heartbeat calls, so its FAILURE case is the one that
// matters: a bot holding everything must report nothing, and a bot holding
// nothing must report every name rather than an empty slice that reads as fine.
func TestMissingReportsWhatWasStripped(t *testing.T) {
	if got := Missing(RequiredBits()); len(got) != 0 {
		t.Errorf("a guild granting everything reports missing: %v", got)
	}
	if got := Missing(0); len(got) != len(Required) {
		t.Errorf("a guild granting nothing reported %d missing, want %d",
			len(got), len(Required))
	}
	// The realistic case: one elevated bit taken away.
	have := RequiredBits() &^ PermManageThreads
	got := Missing(have)
	if len(got) != 1 || got[0] != "MANAGE_THREADS" {
		t.Errorf("stripping MANAGE_THREADS reported %v, want [MANAGE_THREADS]", got)
	}
}

// Discord takes the bitfield as a decimal STRING because the value outgrew the
// integer a JavaScript client holds exactly. Sending a number is the bug that
// shows up as silently-wrong permissions at the far end.
func TestPermissionsSerialiseAsADecimalString(t *testing.T) {
	if got := PermModerateMembers.String(); got != "1099511627776" {
		t.Errorf("1<<40 rendered as %q", got)
	}
	if strings.ContainsAny(RequiredBits().String(), "xX+e") {
		t.Errorf("the total rendered in some non-decimal form: %q", RequiredBits())
	}
}
