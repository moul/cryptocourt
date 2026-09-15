package binding

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// The store under test, on a real file rather than :memory: — the concurrency
// tests below need two connections to see the same database, which an in-memory
// SQLite gives each connection its own copy of.
func newStore(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	w, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	w.SetMaxOpenConns(1)
	r, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatal(err)
	}
	r.SetMaxOpenConns(4)
	t.Cleanup(func() { w.Close(); r.Close() })

	s, err := NewStore(r, w)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func ctx() context.Context { return context.Background() }

func TestAFlowRoundTripsAndReportsWhatItWasFor(t *testing.T) {
	s := newStore(t)
	nonce, err := s.StartFlow(ctx(), "kourt-1", "meta")
	if err != nil {
		t.Fatal(err)
	}
	if len(nonce) != 48 {
		t.Errorf("nonce is %d hex chars, want 48 (24 bytes)", len(nonce))
	}
	f, err := s.SpendFlow(ctx(), nonce)
	if err != nil {
		t.Fatal(err)
	}
	if f.Chain != "kourt-1" || f.Court != "meta" {
		t.Errorf("flow came back as %+v", f)
	}
}

// SINGLE-USE IS THE WHOLE POINT OF THE NONCE. A second redemption must fail, and
// it must fail differently from an unknown one — "you already used this" and
// "this never existed" are different things to tell somebody.
func TestAFlowIsSpentExactlyOnce(t *testing.T) {
	s := newStore(t)
	nonce, _ := s.StartFlow(ctx(), "kourt-1", "meta")

	if _, err := s.SpendFlow(ctx(), nonce); err != nil {
		t.Fatal(err)
	}
	_, err := s.SpendFlow(ctx(), nonce)
	if !errors.Is(err, ErrFlowSpent) {
		t.Errorf("a replay gave %v, want ErrFlowSpent", err)
	}
	if _, err := s.SpendFlow(ctx(), "deadbeef"); !errors.Is(err, ErrNoFlow) {
		t.Errorf("an invented nonce gave %v, want ErrNoFlow", err)
	}
}

// THE RACE THE CONDITIONAL UPDATE EXISTS FOR. A read-then-write would let two
// simultaneous redirects both see an unspent nonce and both proceed; the whole
// reason SpendFlow is one statement is that this test would otherwise pass twice.
func TestTwoSimultaneousRedemptionsProduceExactlyOneWinner(t *testing.T) {
	s := newStore(t)
	nonce, _ := s.StartFlow(ctx(), "kourt-1", "meta")

	const racers = 8
	var wg sync.WaitGroup
	results := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, results[i] = s.SpendFlow(ctx(), nonce)
		}(i)
	}
	close(start)
	wg.Wait()

	var won int
	for _, err := range results {
		if err == nil {
			won++
		}
	}
	if won != 1 {
		t.Errorf("%d of %d redemptions succeeded; exactly one must", won, racers)
	}
}

func TestAnExpiredFlowIsRefusedAndSaysSo(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	s.SetClock(func() time.Time { return now })
	nonce, _ := s.StartFlow(ctx(), "kourt-1", "meta")

	s.SetClock(func() time.Time { return now.Add(FlowTTL + time.Second) })
	if _, err := s.SpendFlow(ctx(), nonce); !errors.Is(err, ErrFlowStale) {
		t.Errorf("an expired flow gave %v, want ErrFlowStale", err)
	}
	// And it is still distinguishable from unknown, which is why the sweep keeps
	// rows around for a day rather than deleting on expiry.
	if _, err := s.SpendFlow(ctx(), "deadbeef"); !errors.Is(err, ErrNoFlow) {
		t.Errorf("unknown gave %v", err)
	}
}

func TestTheSweepKeepsExpiredFlowsLongEnoughToExplainThem(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	s.SetClock(func() time.Time { return now })
	nonce, _ := s.StartFlow(ctx(), "kourt-1", "meta")

	// An hour after expiry the row is still there, so a returning user is told
	// "expired" rather than "no such thing".
	s.SetClock(func() time.Time { return now.Add(FlowTTL + time.Hour) })
	if n, err := s.SweepFlows(ctx()); err != nil || n != 0 {
		t.Errorf("swept %d rows an hour after expiry (err %v); too eager", n, err)
	}
	if _, err := s.SpendFlow(ctx(), nonce); !errors.Is(err, ErrFlowStale) {
		t.Errorf("after the sweep the flow gave %v", err)
	}

	s.SetClock(func() time.Time { return now.Add(48 * time.Hour) })
	if n, err := s.SweepFlows(ctx()); err != nil || n != 1 {
		t.Errorf("swept %d rows after two days (err %v), want 1", n, err)
	}
}

func TestACourtSlugIsCheckedBeforeAFlowIsMinted(t *testing.T) {
	s := newStore(t)
	for _, c := range []string{"", "my-court", "META", "aaaaaaaaaaaa"} {
		if _, err := s.StartFlow(ctx(), "kourt-1", c); !errors.Is(err, ErrBadCourt) {
			t.Errorf("%q was accepted as a court: %v", c, err)
		}
	}
}

// PRESENCE IS NOT PROGRESS. Record is called for every guild the bot lands in,
// including ones nobody asked for, so it must leave the court unlisted.
func TestRecordingAGuildListsNothing(t *testing.T) {
	s := newStore(t)
	if err := s.Record(ctx(), "kourt-1", "meta", "111111111111111111"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Listed(ctx(), "kourt-1", "meta")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("a merely-recorded guild is listed: %+v", got)
	}
}

func TestListedIsNilForACourtWithNoServer(t *testing.T) {
	s := newStore(t)
	got, err := s.Listed(ctx(), "kourt-1", "covid")
	if err != nil {
		t.Errorf("an absent binding was an error: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v", got)
	}
}

func TestListingMakesTheBindingThePublishedOne(t *testing.T) {
	s := newStore(t)
	const gid = "111111111111111111"
	if err := s.Record(ctx(), "kourt-1", "meta", gid); err != nil {
		t.Fatal(err)
	}
	if err := s.List(ctx(), "kourt-1", "meta", gid, modAddr); err != nil {
		t.Fatal(err)
	}
	got, err := s.Listed(ctx(), "kourt-1", "meta")
	if err != nil || got == nil {
		t.Fatalf("listed returned (%v, %v)", got, err)
	}
	if got.GuildID != gid || got.Signer != modAddr || got.State != StateListed {
		t.Errorf("got %+v", got)
	}
	if got.BoundAt.IsZero() || got.ListedAt.IsZero() {
		t.Errorf("timestamps not recorded: %+v", got)
	}
}

func TestListingAnUnknownGuildIsRefused(t *testing.T) {
	s := newStore(t)
	if err := s.List(ctx(), "kourt-1", "meta", "999999999999999999", modAddr); !errors.Is(err, ErrNoBinding) {
		t.Errorf("listing a guild that was never recorded gave %v", err)
	}
}

// ONE LISTED SERVER PER COURT, and publishing a new one displaces the old in the
// same transaction — between the two statements the court would otherwise have
// no server at all.
func TestANewListingDisplacesTheOldOne(t *testing.T) {
	s := newStore(t)
	const first, second = "111111111111111111", "222222222222222222"
	for _, g := range []string{first, second} {
		if err := s.Record(ctx(), "kourt-1", "meta", g); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.List(ctx(), "kourt-1", "meta", first, modAddr); err != nil {
		t.Fatal(err)
	}
	if err := s.List(ctx(), "kourt-1", "meta", second, modAddr); err != nil {
		t.Fatalf("the second listing failed: %v", err)
	}
	got, _ := s.Listed(ctx(), "kourt-1", "meta")
	if got == nil || got.GuildID != second {
		t.Fatalf("after displacing, listed is %+v", got)
	}
	// And exactly one row is listed. Under a race the same holds for a different
	// reason — see TestSimultaneousListingsLeaveExactlyOneServer, which records
	// what actually provides it.
	var n int
	if err := s.r.QueryRow(
		`SELECT count(*) FROM guild_bindings WHERE chain='kourt-1' AND court='meta' AND state='listed'`).
		Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d rows listed for one court", n)
	}
}

// Two courts may each have a server; the constraint is per court, not global.
func TestTwoCourtsMayEachHaveAServer(t *testing.T) {
	s := newStore(t)
	for i, c := range []string{"meta", "covid"} {
		gid := []string{"111111111111111111", "222222222222222222"}[i]
		if err := s.Record(ctx(), "kourt-1", c, gid); err != nil {
			t.Fatal(err)
		}
		if err := s.List(ctx(), "kourt-1", c, gid, modAddr); err != nil {
			t.Fatalf("%s: %v", c, err)
		}
	}
	for _, c := range []string{"meta", "covid"} {
		if got, _ := s.Listed(ctx(), "kourt-1", c); got == nil {
			t.Errorf("%s lost its listing", c)
		}
	}
}

// AUTO-LEAVE MUST NOT EVICT A COURT'S OWN SERVER. Degraded and delisted rows were
// listed once, so their guild has provisioned channels and a history; leaving one
// because a moderator election ran long would destroy something a court chose.
func TestOnlyNeverListedGuildsAreOfferedForLeaving(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	s.SetClock(func() time.Time { return now.Add(-48 * time.Hour) })

	guilds := map[string]string{
		"111111111111111111": StatePending,
		"222222222222222222": StateListed,
		"333333333333333333": StateDegraded,
		"444444444444444444": StateDelisted,
	}
	for gid, state := range guilds {
		if err := s.Record(ctx(), "kourt-1", "meta", gid); err != nil {
			t.Fatal(err)
		}
		if state == StateListed {
			if err := s.List(ctx(), "kourt-1", "meta", gid, modAddr); err != nil {
				t.Fatal(err)
			}
		} else if state != StatePending {
			if err := s.SetState(ctx(), "kourt-1", gid, state, "test"); err != nil {
				t.Fatal(err)
			}
		}
	}

	s.SetClock(func() time.Time { return now })
	out, err := s.Unlisted(ctx(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].GuildID != "111111111111111111" {
		var got []string
		for _, b := range out {
			got = append(got, b.GuildID)
		}
		t.Errorf("offered %v for leaving; only the never-listed pending one should be", got)
	}
}

func TestAFreshPendingGuildIsNotYetOfferedForLeaving(t *testing.T) {
	s := newStore(t)
	if err := s.Record(ctx(), "kourt-1", "meta", "111111111111111111"); err != nil {
		t.Fatal(err)
	}
	out, err := s.Unlisted(ctx(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("a guild recorded seconds ago is already up for eviction: %+v", out)
	}
}

func TestSetStateRecordsWhy(t *testing.T) {
	s := newStore(t)
	const gid = "111111111111111111"
	_ = s.Record(ctx(), "kourt-1", "meta", gid)
	_ = s.List(ctx(), "kourt-1", "meta", gid, modAddr)

	if err := s.SetState(ctx(), "kourt-1", gid, StateDegraded, "MANAGE_THREADS was removed"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Listed(ctx(), "kourt-1", "meta"); got != nil {
		t.Errorf("a degraded binding is still published: %+v", got)
	}
	var note string
	if err := s.r.QueryRow(`SELECT note FROM guild_bindings WHERE guild_id = ?`, gid).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if note != "MANAGE_THREADS was removed" {
		t.Errorf("note = %q", note)
	}
	if err := s.SetState(ctx(), "kourt-1", "999999999999999999", StateDegraded, "x"); !errors.Is(err, ErrNoBinding) {
		t.Errorf("setting state on an unknown guild gave %v", err)
	}
}

// TWO MODERATORS PUBLISHING AT THE SAME INSTANT LEAVE ONE SERVER, NOT TWO.
//
// WHAT THIS DOES NOT PROVE, stated because the obvious reading is wrong: it does
// not exercise the unique index. Drop guild_bindings_one_listed and this still
// passes — measured, six racers in one process and two racers on two independent
// writer handles, every run one listed row. The guarantee comes from List()
// delisting and listing inside one transaction, plus SQLite serialising write
// transactions, so the loser simply runs second and displaces the winner.
//
// The test is worth keeping anyway: the invariant is what callers depend on, and
// it should hold no matter which mechanism is currently providing it. It is the
// claim about the index that was wrong, not the assertion.
func TestSimultaneousListingsLeaveExactlyOneServer(t *testing.T) {
	s := newStore(t)
	const racers = 6
	gids := make([]string, racers)
	for i := range gids {
		gids[i] = "10000000000000000" + string(rune('0'+i))
		if err := s.Record(ctx(), "kourt-1", "meta", gids[i]); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, g := range gids {
		wg.Add(1)
		go func(g string) {
			defer wg.Done()
			<-start
			_ = s.List(ctx(), "kourt-1", "meta", g, modAddr)
		}(g)
	}
	close(start)
	wg.Wait()

	var n int
	if err := s.r.QueryRow(
		`SELECT count(*) FROM guild_bindings WHERE chain='kourt-1' AND court='meta' AND state='listed'`).
		Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d servers listed for one court after a %d-way race; the unique "+
			"index did not hold", n, racers)
	}
	if got, err := s.Listed(ctx(), "kourt-1", "meta"); err != nil || got == nil {
		t.Errorf("after the race the court has no server at all: (%v, %v)", got, err)
	}
}

// A GUILD MUST NOT BE ABLE TO CHANGE WHICH COURT IT IS PUBLISHED FOR.
//
// Record upserts on (chain, guild_id), and the first version updated `court`
// unconditionally. That is a privilege escalation and not a small one: a guild
// already LISTED for one court could be re-pointed at another by anybody who
// could complete an OAuth round trip naming the second court — Discord returns
// the guild object happily for a bot that is already installed, so no moderator
// of the second court signs anything and the row keeps its listed state while
// changing whose server it is.
//
// The rule is that a pending row may be re-aimed (nothing depends on it yet) and
// a row that reached listed may not. Moving one means delisting it first, which
// is a decision with a moderator behind it.
func TestRecordingCannotMoveAPublishedServerToAnotherCourt(t *testing.T) {
	s := newStore(t)
	const gid = "111111111111111111"
	if err := s.Record(ctx(), "kourt-1", "meta", gid); err != nil {
		t.Fatal(err)
	}
	if err := s.List(ctx(), "kourt-1", "meta", gid, modAddr); err != nil {
		t.Fatal(err)
	}

	// The attack: a flow for another court, completed with the same guild.
	_ = s.Record(ctx(), "kourt-1", "covid", gid)

	if b, _ := s.Listed(ctx(), "kourt-1", "covid"); b != nil {
		t.Errorf("covid acquired meta's published server without any moderator of "+
			"covid signing for it: %+v", b)
	}
	b, err := s.Listed(ctx(), "kourt-1", "meta")
	if err != nil || b == nil {
		t.Fatalf("meta lost its own server: (%v, %v)", b, err)
	}
	if b.GuildID != gid {
		t.Errorf("meta's server changed to %s", b.GuildID)
	}
}

// The permissive half: a guild nobody has published yet may be re-aimed, because
// nothing depends on it and a reader who started the wrong flow should not be
// stuck with a dead row.
func TestAPendingGuildMayStillBeReAimed(t *testing.T) {
	s := newStore(t)
	const gid = "111111111111111111"
	if err := s.Record(ctx(), "kourt-1", "meta", gid); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx(), "kourt-1", "covid", gid); err != nil {
		t.Fatal(err)
	}
	var court string
	if err := s.r.QueryRow(`SELECT court FROM guild_bindings WHERE guild_id = ?`, gid).Scan(&court); err != nil {
		t.Fatal(err)
	}
	if court != "covid" {
		t.Errorf("a pending guild was not re-aimed: court = %q", court)
	}
}

// LISTING MUST MATCH THE COURT THE ROW IS BOUND TO, not just the guild.
//
// The delist half of List() filtered on (chain, court); the list half filtered on
// (chain, guild_id) alone. So a moderator of court B, holding a valid signature
// for B and passing every check in /claim, could call List(chain, "B", G, …) for a
// guild G whose row was bound to court A — and the UPDATE would publish A's row,
// with B's signer on it. Court A acquires a Discord chosen by somebody who does
// not moderate it, which is the front-running this whole design exists to prevent,
// reached through a different door.
func TestListingCannotPublishARowBoundToAnotherCourt(t *testing.T) {
	s := newStore(t)
	const gid = "111111111111111111"
	// The guild is bound to meta, and merely pending.
	if err := s.Record(ctx(), "kourt-1", "meta", gid); err != nil {
		t.Fatal(err)
	}

	// A moderator of covid tries to publish it for covid.
	err := s.List(ctx(), "kourt-1", "covid", gid, modAddr)
	if !errors.Is(err, ErrNoBinding) {
		t.Errorf("listing a foreign court's guild returned %v, want ErrNoBinding", err)
	}
	if b, _ := s.Listed(ctx(), "kourt-1", "covid"); b != nil {
		t.Errorf("covid published a guild bound to meta: %+v", b)
	}
	if b, _ := s.Listed(ctx(), "kourt-1", "meta"); b != nil {
		t.Errorf("meta was published by a moderator of another court: %+v", b)
	}
}

// /api/guild/start IS AN UNAUTHENTICATED WRITE and has to be, so it needs a
// ceiling: without one it is free unbounded insertion into the database the chat
// service also writes to, and contention on a serialised writer costs more than
// the disk does.
func TestOneCourtCannotMintFlowsForever(t *testing.T) {
	s := newStore(t)
	for i := 0; i < MaxOpenFlows; i++ {
		if _, err := s.StartFlow(ctx(), "kourt-1", "meta"); err != nil {
			t.Fatalf("honest flow %d refused: %v", i, err)
		}
	}
	if _, err := s.StartFlow(ctx(), "kourt-1", "meta"); !errors.Is(err, ErrTooManyFlows) {
		t.Errorf("flow %d was accepted: %v", MaxOpenFlows+1, err)
	}
}

// THE CAP IS PER COURT, so filling one court's bucket cannot stop another court
// being published. A global cap would make this a denial of service against every
// court at once, which is worse than the thing it defends against.
func TestFillingOneCourtsBucketDoesNotBlockAnother(t *testing.T) {
	s := newStore(t)
	for i := 0; i < MaxOpenFlows; i++ {
		if _, err := s.StartFlow(ctx(), "kourt-1", "meta"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.StartFlow(ctx(), "kourt-1", "covid"); err != nil {
		t.Errorf("a full meta bucket blocked covid: %v", err)
	}
}

// Spent and expired flows do not count: the ceiling is on authorisations actually
// in flight, not on how many a court has ever started.
func TestSpentAndExpiredFlowsDoNotFillTheBucket(t *testing.T) {
	s := newStore(t)
	now := time.Now()
	s.SetClock(func() time.Time { return now })

	var first string
	for i := 0; i < MaxOpenFlows; i++ {
		n, err := s.StartFlow(ctx(), "kourt-1", "meta")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = n
		}
	}
	// Spending one frees a slot.
	if _, err := s.SpendFlow(ctx(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartFlow(ctx(), "kourt-1", "meta"); err != nil {
		t.Errorf("spending a flow did not free its slot: %v", err)
	}

	// And so does time.
	s.SetClock(func() time.Time { return now.Add(FlowTTL + time.Minute) })
	for i := 0; i < MaxOpenFlows; i++ {
		if _, err := s.StartFlow(ctx(), "kourt-1", "meta"); err != nil {
			t.Fatalf("expired flows still fill the bucket at %d: %v", i, err)
		}
	}
}

// THE CAP MUST HOLD UNDER A BURST, which is the only condition it matters in.
// It was a SELECT on the reader pool then an INSERT on the writer — two
// unserialised statements, so a crowd could all read "under the limit" before any
// of them committed. A limit a crowd walks through is not a limit.
func TestTheFlowCapHoldsUnderConcurrentStarts(t *testing.T) {
	s := newStore(t)
	const racers = 40
	var wg sync.WaitGroup
	got := make([]error, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, got[i] = s.StartFlow(ctx(), "kourt-1", "meta")
		}(i)
	}
	close(start)
	wg.Wait()

	var ok int
	for _, err := range got {
		if err == nil {
			ok++
		} else if !errors.Is(err, ErrTooManyFlows) {
			t.Errorf("unexpected error: %v", err)
		}
	}
	var stored int
	if err := s.r.QueryRow(`SELECT count(*) FROM guild_flows`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored > MaxOpenFlows {
		t.Errorf("%d flows stored for a cap of %d: the burst walked through it",
			stored, MaxOpenFlows)
	}
	if ok != stored {
		t.Errorf("%d starts reported success but %d rows exist", ok, stored)
	}
}
