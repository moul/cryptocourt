package binding

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jaekwon/kourt/internal/gnorpc"
	"github.com/jaekwon/kourt/internal/guild"
)

// A Discord that answers whatever the test wants about the fleet.
type fakeFleet struct {
	perms map[string]guild.Permission
	err   error
	calls int
}

func (f *fakeFleet) MyGuilds(ctx context.Context) (map[string]guild.Permission, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.perms, nil
}

type beat struct {
	h       *Heartbeat
	s       *Store
	fleet   *fakeFleet
	isMod   bool
	down    bool
	noCourt bool
	now     time.Time
}

const hbGuild = "111111111111111111"

func newBeat(t *testing.T) *beat {
	t.Helper()
	b := &beat{s: newStore(t), isMod: true, now: time.Now(),
		fleet: &fakeFleet{perms: map[string]guild.Permission{hbGuild: guild.RequiredBits()}}}

	chain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if b.down {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		if b.noCourt {
			fmt.Fprint(w, `{"jsonrpc":"2.0","result":{"response":{"ResponseBase":{`+
				`"Error":{"value":"x"},"Log":"kourtv2: no such court"}}}}`)
			return
		}
		out := "(false bool)"
		if b.isMod {
			out = "(true bool)"
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","result":{"response":{"ResponseBase":{"Data":%q}}}}`,
			base64.StdEncoding.EncodeToString([]byte(out)))
	}))
	t.Cleanup(chain.Close)

	b.s.SetClock(func() time.Time { return b.now })
	b.h = &Heartbeat{
		Store:    b.s,
		Verifier: &Verifier{Node: &gnorpc.Node{RPC: chain.URL, HTTP: chain.Client()}},
		Discord:  b.fleet,
	}
	b.h.SetClock(func() time.Time { return b.now })
	return b
}

// listed puts a healthy published binding in place.
func (b *beat) listed(t *testing.T, court string) {
	t.Helper()
	if err := b.s.Record(ctx(), "kourt-1", court, hbGuild); err != nil {
		t.Fatal(err)
	}
	if err := b.s.List(ctx(), "kourt-1", court, hbGuild, modAddr); err != nil {
		t.Fatal(err)
	}
}

func (b *beat) state(t *testing.T) string {
	t.Helper()
	var s string
	if err := b.s.r.QueryRow(`SELECT state FROM guild_bindings WHERE guild_id = ?`, hbGuild).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAHealthyListingIsLeftAlone(t *testing.T) {
	b := newBeat(t)
	b.listed(t, "meta")
	r, err := b.h.Once(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if r.Checked != 1 || r.Degraded != 0 || r.Delisted != 0 {
		t.Errorf("a healthy listing produced %+v", r)
	}
	if got := b.state(t); got != StateListed {
		t.Errorf("state = %q", got)
	}
}

// THE FAIL DIRECTION. An unreachable Discord is the whole fleet looking absent,
// and acting on it would delist every court on the site at once — an outage of
// ours caused by somebody else's.
func TestAnUnreachableDiscordChangesNothing(t *testing.T) {
	b := newBeat(t)
	b.listed(t, "meta")
	b.fleet.err = errors.New("dial tcp: connection refused")

	r, err := b.h.Once(ctx())
	if err == nil {
		t.Fatal("an unreachable Discord was not reported")
	}
	if r.Degraded != 0 || r.Delisted != 0 {
		t.Errorf("it changed things anyway: %+v", r)
	}
	if got := b.state(t); got != StateListed {
		t.Errorf("state = %q, want it untouched", got)
	}
}

// And the same for the chain, which is checked per binding rather than once — so
// this is the case where a partial outage must not take a listing down.
func TestAnUnreachableChainLeavesTheListingAlone(t *testing.T) {
	b := newBeat(t)
	b.listed(t, "meta")
	b.down = true

	r, err := b.h.Once(ctx())
	if err != nil {
		t.Fatalf("a chain outage aborted the sweep: %v", err)
	}
	if r.Degraded != 0 {
		t.Errorf("it degraded on silence: %+v", r)
	}
	if got := b.state(t); got != StateListed {
		t.Errorf("state = %q", got)
	}
}

func TestAKickedBotDegradesTheListing(t *testing.T) {
	b := newBeat(t)
	b.listed(t, "meta")
	b.fleet.perms = map[string]guild.Permission{} // gone

	r, _ := b.h.Once(ctx())
	if r.Degraded != 1 {
		t.Errorf("result = %+v", r)
	}
	if got := b.state(t); got != StateDegraded {
		t.Errorf("state = %q", got)
	}
	// And the court page stops showing it immediately — degraded is not listed.
	if l, _ := b.s.Listed(ctx(), "kourt-1", "meta"); l != nil {
		t.Errorf("a degraded binding is still published: %+v", l)
	}
}

// A STRIPPED PERMISSION IS NAMED, because this ends up in front of a server owner
// who has to restore it and a bitfield is a number somebody has to decode.
func TestAStrippedPermissionIsNamedInTheNote(t *testing.T) {
	b := newBeat(t)
	b.listed(t, "meta")
	b.fleet.perms[hbGuild] = guild.RequiredBits() &^ guild.PermManageThreads

	if _, err := b.h.Once(ctx()); err != nil {
		t.Fatal(err)
	}
	var note string
	if err := b.s.r.QueryRow(`SELECT note FROM guild_bindings WHERE guild_id = ?`, hbGuild).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "MANAGE_THREADS") {
		t.Errorf("note = %q; it must name what to restore", note)
	}
}

// The other half of the publish rule, enforced continuously: a signer removed
// from the court's moderator set loses the listing they made.
func TestASignerWhoStopsModeratingLosesTheListing(t *testing.T) {
	b := newBeat(t)
	b.listed(t, "meta")
	b.isMod = false

	if _, err := b.h.Once(ctx()); err != nil {
		t.Fatal(err)
	}
	if got := b.state(t); got != StateDegraded {
		t.Errorf("state = %q", got)
	}
}

// A transient failure that clears must not need a human.
func TestADegradedListingComesBackByItself(t *testing.T) {
	b := newBeat(t)
	b.listed(t, "meta")
	b.fleet.perms = map[string]guild.Permission{}
	if _, err := b.h.Once(ctx()); err != nil {
		t.Fatal(err)
	}
	if b.state(t) != StateDegraded {
		t.Fatal("setup: it did not degrade")
	}

	b.fleet.perms = map[string]guild.Permission{hbGuild: guild.RequiredBits()}
	r, err := b.h.Once(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if r.Restored != 1 {
		t.Errorf("result = %+v", r)
	}
	if got := b.state(t); got != StateListed {
		t.Errorf("state = %q", got)
	}
}

// THE GRACE CLOCK RUNS FROM THE FIRST FAILURE, NOT FROM THE LAST SWEEP. Re-stamping
// it every few minutes would push the deadline forward forever and a broken
// listing would never be cleaned up.
func TestTheGraceClockIsNotResetByEverySweep(t *testing.T) {
	b := newBeat(t)
	b.h.Grace = 48 * time.Hour
	b.listed(t, "meta")
	b.fleet.perms = map[string]guild.Permission{}

	start := b.now
	// Degrade, then sweep repeatedly over three days.
	for i := 0; i < 6; i++ {
		b.now = start.Add(time.Duration(i) * 12 * time.Hour)
		if _, err := b.h.Once(ctx()); err != nil {
			t.Fatal(err)
		}
	}
	if got := b.state(t); got != StateDelisted {
		t.Errorf("state = %q after three days of a 48h grace; the clock is being "+
			"reset by each sweep", got)
	}
}

func TestADegradedListingIsKeptThroughItsGrace(t *testing.T) {
	b := newBeat(t)
	b.h.Grace = 48 * time.Hour
	b.listed(t, "meta")
	b.fleet.perms = map[string]guild.Permission{}

	start := b.now
	if _, err := b.h.Once(ctx()); err != nil {
		t.Fatal(err)
	}
	b.now = start.Add(47 * time.Hour)
	r, err := b.h.Once(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if r.Delisted != 0 || b.state(t) != StateDegraded {
		t.Errorf("delisted inside the grace window: %+v, state %q", r, b.state(t))
	}
}

// A court that is gone is an ANSWER, not a silence, and is the one chain error
// that degrades rather than being left alone.
func TestACourtThatNoLongerExistsDegrades(t *testing.T) {
	b := newBeat(t)
	b.listed(t, "meta")
	b.noCourt = true

	if _, err := b.h.Once(ctx()); err != nil {
		t.Fatal(err)
	}
	if got := b.state(t); got != StateDegraded {
		t.Errorf("state = %q", got)
	}
}

// Delisted rows are over. Polling them is how a sweep's cost grows without bound
// against a shared request budget.
func TestDelistedBindingsAreNotSweptAgain(t *testing.T) {
	b := newBeat(t)
	b.listed(t, "meta")
	if err := b.s.SetState(ctx(), "kourt-1", hbGuild, StateDelisted, "by hand"); err != nil {
		t.Fatal(err)
	}
	r, err := b.h.Once(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if r.Checked != 0 {
		t.Errorf("it checked %d delisted binding(s)", r.Checked)
	}
	if b.fleet.calls != 0 {
		t.Errorf("it called Discord %d time(s) with nothing to check", b.fleet.calls)
	}
}

// ONE DISCORD READ FOR THE WHOLE FLEET, not one per court. The budget is 50
// requests/second shared with everything else the bot does.
func TestTheSweepReadsDiscordOncePerRun(t *testing.T) {
	b := newBeat(t)
	for _, court := range []string{"meta", "covid", "ledger"} {
		gid := "1111111111111111" + fmt.Sprintf("%02d", len(court))
		if err := b.s.Record(ctx(), "kourt-1", court, gid); err != nil {
			t.Fatal(err)
		}
		if err := b.s.List(ctx(), "kourt-1", court, gid, modAddr); err != nil {
			t.Fatal(err)
		}
		b.fleet.perms[gid] = guild.RequiredBits()
	}
	r, err := b.h.Once(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if r.Checked != 3 {
		t.Errorf("checked %d", r.Checked)
	}
	if b.fleet.calls != 1 {
		t.Errorf("it read Discord %d times for 3 courts", b.fleet.calls)
	}
}

type fakeLeaver struct {
	left []string
	err  error
}

func (f *fakeLeaver) Leave(id string) error { f.left = append(f.left, id); return f.err }

// A GUILD NOBODY CLAIMED IS LEFT. Presence proves nothing and grants nothing, but
// it still costs standing exposure — the bot can enumerate that guild for as long
// as it sits in it.
func TestAGuildNobodyClaimedIsLeft(t *testing.T) {
	b := newBeat(t)
	start := b.now
	b.now = start.Add(-48 * time.Hour)
	if err := b.s.Record(ctx(), "kourt-1", "meta", hbGuild); err != nil {
		t.Fatal(err)
	}
	b.now = start

	l := &fakeLeaver{}
	n, err := b.h.Sweep(ctx(), l)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(l.left) != 1 || l.left[0] != hbGuild {
		t.Errorf("left %d guild(s) %v", n, l.left)
	}
	if got := b.state(t); got != StateDelisted {
		t.Errorf("state = %q; a left guild must be closed or the next sweep sees it again", got)
	}
}

// THE RULE THAT MATTERS MORE THAN THE LEAVING. A guild that was ever listed has
// provisioned channels and a history; leaving one because a moderator election ran
// long would destroy something a court chose.
func TestASweepNeverLeavesAGuildThatWasEverListed(t *testing.T) {
	for _, state := range []string{StateListed, StateDegraded, StateDelisted} {
		b := newBeat(t)
		start := b.now
		b.now = start.Add(-48 * time.Hour)
		b.listed(t, "meta")
		if state != StateListed {
			if err := b.s.SetState(ctx(), "kourt-1", hbGuild, state, "test"); err != nil {
				t.Fatal(err)
			}
		}
		b.now = start

		l := &fakeLeaver{}
		if _, err := b.h.Sweep(ctx(), l); err != nil {
			t.Fatal(err)
		}
		if len(l.left) != 0 {
			t.Errorf("%s: left %v", state, l.left)
		}
	}
}

func TestAFreshlyAddedGuildIsNotLeftYet(t *testing.T) {
	b := newBeat(t)
	if err := b.s.Record(ctx(), "kourt-1", "meta", hbGuild); err != nil {
		t.Fatal(err)
	}
	l := &fakeLeaver{}
	if _, err := b.h.Sweep(ctx(), l); err != nil {
		t.Fatal(err)
	}
	if len(l.left) != 0 {
		t.Errorf("a guild added seconds ago was left: %v", l.left)
	}
}

// A guild that is already gone answers 404 to a leave. The row must still close,
// or every sweep forever will try it again.
func TestAFailedLeaveStillClosesTheRow(t *testing.T) {
	b := newBeat(t)
	start := b.now
	b.now = start.Add(-48 * time.Hour)
	if err := b.s.Record(ctx(), "kourt-1", "meta", hbGuild); err != nil {
		t.Fatal(err)
	}
	b.now = start

	if _, err := b.h.Sweep(ctx(), &fakeLeaver{err: errors.New("404 Unknown Guild")}); err != nil {
		t.Fatal(err)
	}
	if got := b.state(t); got != StateDelisted {
		t.Errorf("state = %q after a failed leave", got)
	}
}

// JITTER IS NOT COSMETIC: a fixed period is a window an owner can work inside —
// strip the permissions after a sweep, restore them before the next, and every
// check the site makes says healthy.
func TestTheCadenceIsJittered(t *testing.T) {
	const base = time.Hour
	seen := map[time.Duration]bool{}
	var min, max time.Duration = 1 << 62, 0
	for i := 0; i < 200; i++ {
		d := jitter(base)
		seen[d] = true
		if d < min {
			min = d
		}
		if d > max {
			max = d
		}
	}
	if len(seen) < 100 {
		t.Errorf("200 draws produced only %d distinct delays; this is not jitter", len(seen))
	}
	// Within a quarter either way, so the average cadence stays honest.
	if min < base-base/4 || max > base+base/4 {
		t.Errorf("spread %v..%v is outside a quarter of %v", min, max, base)
	}
	if min == max {
		t.Error("every draw was identical")
	}
}

func TestAZeroIntervalDoesNotSpin(t *testing.T) {
	b := newBeat(t)
	done := make(chan struct{})
	go func() { b.h.Run(context.Background(), 0, nil); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Run(0) did not return; a misconfigured interval must not spin")
	}
}

func TestRunStopsWithItsContext(t *testing.T) {
	b := newBeat(t)
	c, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { b.h.Run(c, time.Hour, nil); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Run did not stop when its context was cancelled")
	}
}
