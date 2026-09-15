package binding

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jaekwon/kourt/internal/guild"
)

// THE TWO TABLES ARE A FLOW, NOT A RECORD.
//
// `flows` is the short-lived half: a nonce this service minted, tied to the court
// it was minted for, spent exactly once. `bindings` is the durable half: which
// guild a court's moderators chose, and what state that choice is in.
//
// Splitting them is the point. A flow is evidence that a particular browser
// started a particular authorisation a few minutes ago; a binding is a standing
// claim about a court. Keeping the evidence in the same row as the claim is how a
// replayed nonce quietly becomes a second listing.
const schema = `
CREATE TABLE IF NOT EXISTS guild_flows (
  nonce      TEXT PRIMARY KEY,
  chain      TEXT NOT NULL,
  court      TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  spent_at   INTEGER
);
CREATE INDEX IF NOT EXISTS guild_flows_sweep ON guild_flows (expires_at);

CREATE TABLE IF NOT EXISTS guild_bindings (
  chain      TEXT NOT NULL,
  court      TEXT NOT NULL,
  guild_id   TEXT NOT NULL,
  state      TEXT NOT NULL,
  signer     TEXT,
  invite     TEXT,
  bound_at   INTEGER NOT NULL,
  listed_at  INTEGER,
  checked_at INTEGER,
  note       TEXT,
  PRIMARY KEY (chain, guild_id)
);
-- ONE LISTED SERVER PER COURT.
--
-- MEASURED, and the measurement corrected what this comment first claimed. The
-- index was written as "the only thing that holds when two moderators publish at
-- the same instant". It is not: List() delists the court's other rows and lists
-- its own inside ONE transaction, and SQLite serialises write transactions — so
-- the second publisher runs after the first and displaces it. Dropping this index
-- and racing six publishers in one process, then two publishers on two
-- independent writer handles, both still leave exactly one listed row.
--
-- It is kept as a backstop against a writer that does NOT go through List() — a
-- migration, an admin repair, a second implementation in the gateway process.
-- Those are the cases with no transaction ordering to rely on, and a court with
-- two published servers is not a state anything downstream knows how to render.
CREATE UNIQUE INDEX IF NOT EXISTS guild_bindings_one_listed
  ON guild_bindings (chain, court) WHERE state = 'listed';
CREATE INDEX IF NOT EXISTS guild_bindings_court ON guild_bindings (chain, court);
`

// State is where a binding stands. The words are stored, not their ordinals, so a
// database is readable without this file.
const (
	// StatePending — the bot is in the guild and nothing else is true yet. Grants
	// nothing at all: no provisioning, no docket, no link on kourt.xyz.
	StatePending = "pending"
	// StateListed — a court moderator signed for this guild and the chain agreed.
	// The only state in which the bot acts or the site publishes.
	StateListed = "listed"
	// StateDegraded — was listed, and something has since stopped being true.
	// The link is hidden but the binding and its provisioned state are kept,
	// because the usual cause is temporary.
	StateDegraded = "degraded"
	// StateDelisted — over: the grace ran out while degraded, or the guild was
	// left unclaimed. The row is kept rather than deleted so its history stays
	// readable and the sweep stops looking at it.
	//
	// IT DOES NOT BLOCK A FRESH CLAIM, and an earlier version of this comment
	// said it did. List() filters on (chain, guild_id, court) and not on state,
	// so a moderator who fixes whatever broke can publish the same guild again —
	// which is the right behaviour for the two causes above, both of which are
	// "it stopped working", not "it was taken away".
	//
	// The day a FOR-CAUSE delist exists — a directory-admin takedown — it needs a
	// state of its own rather than this one, precisely because a still-valid
	// moderator must not be able to undo it by re-running a claim. Written down
	// here because the code cannot express a distinction that has no second state
	// to express it with.
	StateDelisted = "delisted"
)

var (
	ErrNoFlow    = errors.New("binding: no such flow")
	ErrFlowSpent = errors.New("binding: that flow was already used")
	ErrFlowStale = errors.New("binding: that flow has expired")
	ErrNoBinding = errors.New("binding: no such binding")
	// ErrGuildTaken is a guild already spoken for by another court. It is a
	// refusal rather than a move: see Record.
	ErrGuildTaken = errors.New("binding: that guild is bound to another court")
	// ErrTooManyFlows is a court with more authorisations in flight than anybody
	// could be conducting. See MaxOpenFlows.
	ErrTooManyFlows = errors.New("binding: too many authorisations already in flight for that court")
)

// MaxOpenFlows bounds the unspent, unexpired flows one court may have at once.
//
// /api/guild/start IS AN UNAUTHENTICATED WRITE, which is unavoidable: a
// moderator has to be able to begin before they have proved anything. So the
// endpoint mints a row for anybody who asks, and without a bound that is free
// unbounded insertion into the database the chat service also writes to —
// contention on a serialised writer matters more here than the disk does.
//
// SIXTY-FOUR is far above any honest use. A flow lives ten minutes; one court
// having sixty-four people mid-authorisation inside one window is not a thing
// that happens, and a moderator who somehow hits it waits ten minutes rather
// than losing anything. The cap is PER COURT so that filling one court's bucket
// cannot stop any other court being published.
const MaxOpenFlows = 64

// FlowTTL is how long a started authorisation stays redeemable.
//
// Ten minutes is a person leaving the page, reading Discord's consent screen,
// picking a server from a list and clicking through. It is deliberately not an
// hour: this value is a bearer token in a URL, and its window is the window in
// which a copied link is worth something.
const FlowTTL = 10 * time.Minute

// Store is the bridge's half of the service database.
//
// It takes both handles because they are not interchangeable: the listing lookup
// runs on every court page and belongs on the reader's pool of four, while writes
// belong on the serialised writer. Reading through the writer is what internal/archive
// does, which is survivable at upload rates and would not be at page-load rates.
type Store struct {
	r, w *sql.DB
	now  func() time.Time
}

func NewStore(r, w *sql.DB) (*Store, error) {
	if _, err := w.Exec(schema); err != nil {
		return nil, fmt.Errorf("binding schema: %w", err)
	}
	return &Store{r: r, w: w, now: time.Now}, nil
}

// SetClock replaces the clock, for tests. Named rather than a struct field so it
// cannot be set by accident from another package.
func (s *Store) SetClock(f func() time.Time) { s.now = f }

// StartFlow mints a nonce and records what it is for.
//
// THE NONCE IS MINTED HERE AND NOWHERE ELSE. A client-chosen value verifies
// nothing — an attacker composes their own and it validates perfectly — so the
// only thing that makes a returning redirect evidence of anything is that this
// service issued the value, stored it, and will refuse it the second time.
func (s *Store) StartFlow(ctx context.Context, chain, court string) (string, error) {
	if !guild.ValidCourt(court) {
		return "", fmt.Errorf("%w: %q", ErrBadCourt, court)
	}
	now := s.now()
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(b)

	// THE COUNT AND THE INSERT ARE ONE STATEMENT, on one connection.
	//
	// They were a SELECT on the reader pool followed by an INSERT on the writer,
	// which is two unserialised statements and therefore not a cap: a burst of
	// concurrent starts for the same court could all read "63 open" before any of
	// them committed. Bounded damage — the rows still expire — but a limit that a
	// crowd walks through is not a limit. As one INSERT … SELECT the writer's own
	// serialisation does the work.
	res, err := s.w.ExecContext(ctx,
		`INSERT INTO guild_flows (nonce, chain, court, created_at, expires_at)
		 SELECT ?,?,?,?,?
		  WHERE (SELECT count(*) FROM guild_flows
		          WHERE chain = ? AND court = ? AND spent_at IS NULL AND expires_at > ?)
		        < ?`,
		nonce, chain, court, now.Unix(), now.Add(FlowTTL).Unix(),
		chain, court, now.Unix(), MaxOpenFlows)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", fmt.Errorf("%w: %d already open", ErrTooManyFlows, MaxOpenFlows)
	}
	return nonce, nil
}

// Flow is what a redeemed nonce was for.
type Flow struct {
	Chain string
	Court string
}

// SpendFlow redeems a nonce exactly once and reports what it was minted for.
//
// SINGLE-USE IS ENFORCED BY THE UPDATE, NOT BY A READ-THEN-WRITE. The conditional
// UPDATE is atomic; a SELECT followed by an UPDATE is two statements with a gap,
// and the gap is the whole attack — two redirects arriving together would both
// see an unspent nonce and both proceed.
//
// It returns the flow's own court rather than trusting anything in the request,
// which is the other half of the same idea: the caller learns what was authorised
// from what was minted, not from what came back.
func (s *Store) SpendFlow(ctx context.Context, nonce string) (Flow, error) {
	now := s.now().Unix()
	res, err := s.w.ExecContext(ctx,
		`UPDATE guild_flows SET spent_at = ?
		  WHERE nonce = ? AND spent_at IS NULL AND expires_at > ?`,
		now, nonce, now)
	if err != nil {
		return Flow{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return Flow{}, err
	}
	if n == 1 {
		var f Flow
		if err := s.r.QueryRowContext(ctx,
			`SELECT chain, court FROM guild_flows WHERE nonce = ?`, nonce).
			Scan(&f.Chain, &f.Court); err != nil {
			return Flow{}, err
		}
		return f, nil
	}

	// It did not apply. Say WHY, because the three reasons are operationally
	// different: a spent nonce is a replay or a double-click, an expired one is
	// somebody who left the tab open, and an unknown one is a fabricated link.
	var spent sql.NullInt64
	var expires int64
	switch err := s.r.QueryRowContext(ctx,
		`SELECT spent_at, expires_at FROM guild_flows WHERE nonce = ?`, nonce).
		Scan(&spent, &expires); {
	case errors.Is(err, sql.ErrNoRows):
		return Flow{}, ErrNoFlow
	case err != nil:
		return Flow{}, err
	case spent.Valid:
		return Flow{}, ErrFlowSpent
	default:
		return Flow{}, ErrFlowStale
	}
}

// SweepFlows deletes flows that expired more than a day ago.
//
// Not immediately on expiry: a spent or stale nonce presented again should get
// ErrFlowSpent or ErrFlowStale rather than ErrNoFlow, because "you already used
// this" is a different thing to tell somebody than "this never existed", and a
// row that has been deleted cannot tell them apart.
func (s *Store) SweepFlows(ctx context.Context) (int64, error) {
	cutoff := s.now().Add(-24 * time.Hour).Unix()
	res, err := s.w.ExecContext(ctx, `DELETE FROM guild_flows WHERE expires_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Binding is one guild's relationship to one court.
type Binding struct {
	Chain     string
	Court     string
	GuildID   string
	State     string
	Signer    string
	Invite    string
	BoundAt   time.Time
	ListedAt  time.Time
	CheckedAt time.Time
}

// Record notes that the bot is in a guild, in the pending state.
//
// PENDING GRANTS NOTHING, and this function is the reason that has to be said out
// loud: it is called for every guild the bot lands in, including the ones nobody
// asked for. Anyone can build the authorize URL from the client id in the page
// source and install the bot into a guild they control, so presence is not
// evidence of anything and must not be treated as a step toward listing.
// A GUILD MAY BE RE-AIMED ONLY WHILE IT IS PENDING, and that WHERE clause is
// load-bearing. Without it the upsert moved `court` unconditionally, which is a
// privilege escalation: Discord returns the guild object happily for a bot that
// is already installed, so anybody who could complete an OAuth round trip naming
// a second court could re-point a guild that was already LISTED for a first one.
// The row kept its listed state and even its original signer; the first court
// lost its server and the second gained one with no moderator of it signing
// anything. Measured — TestRecordingCannotMoveAPublishedServerToAnotherCourt
// fails without this clause and reports exactly that.
//
// Pending rows stay re-aimable because nothing depends on them and a reader who
// started the wrong flow should not be stuck with a dead row.
func (s *Store) Record(ctx context.Context, chain, court, guildID string) error {
	if !guild.ValidCourt(court) {
		return fmt.Errorf("%w: %q", ErrBadCourt, court)
	}
	if _, err := s.w.ExecContext(ctx,
		`INSERT INTO guild_bindings (chain, court, guild_id, state, bound_at)
		 VALUES (?,?,?,?,?)
		 ON CONFLICT (chain, guild_id) DO UPDATE SET court = excluded.court
		   WHERE guild_bindings.state = ?`,
		chain, court, guildID, StatePending, s.now().Unix(), StatePending); err != nil {
		return err
	}

	// AND IT SAYS SO RATHER THAN SILENTLY DOING NOTHING. A refused re-aim leaves
	// the row untouched, which from the caller's side is indistinguishable from
	// success — and the caller is an HTTP handler about to tell somebody their
	// server was added. Reading the court back is one indexed lookup and turns a
	// silent no-op into a sentence a person can act on.
	var have string
	switch err := s.r.QueryRowContext(ctx,
		`SELECT court FROM guild_bindings WHERE chain = ? AND guild_id = ?`,
		chain, guildID).Scan(&have); {
	case err != nil:
		return err
	case have != court:
		return fmt.Errorf("%w: it is already bound to %q", ErrGuildTaken, have)
	}
	return nil
}

// List makes a binding the court's published server, displacing whatever held the
// slot.
//
// Both halves happen in ONE TRANSACTION because the unique index permits exactly
// one listed row per court: displacing and listing are two statements that are
// only ever correct together, and between them the court has no server at all.
func (s *Store) List(ctx context.Context, chain, court, guildID, signer string) error {
	tx, err := s.w.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := s.now().Unix()
	if _, err := tx.ExecContext(ctx,
		`UPDATE guild_bindings SET state = ?, note = ?
		  WHERE chain = ? AND court = ? AND state = ? AND guild_id <> ?`,
		StateDelisted, "replaced by a newer binding", chain, court, StateListed, guildID); err != nil {
		return err
	}
	// AND court = ? IS LOAD-BEARING, and its absence was an escalation.
	//
	// The delist statement above has always filtered on the court; this one
	// filtered on (chain, guild_id) alone. So a moderator of court B — holding a
	// valid signature for B, passing every check in /claim — could publish a
	// guild whose row was bound to court A, and the UPDATE would set A's row to
	// listed with B's signer on it. Court A acquires a Discord chosen by somebody
	// who does not moderate it, which is the front-running this design exists to
	// prevent, reached through a different door.
	//
	// A mismatch now affects no rows and surfaces as ErrNoBinding, which is what
	// the caller already handles: the bot is not in a server bound to this court.
	res, err := tx.ExecContext(ctx,
		`UPDATE guild_bindings
		    SET state = ?, signer = ?, listed_at = ?, checked_at = ?, note = NULL
		  WHERE chain = ? AND guild_id = ? AND court = ?`,
		StateListed, signer, now, now, chain, guildID, court)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoBinding
	}
	return tx.Commit()
}

// Listed returns the court's published binding, if it has one.
//
// This is the hot path — it runs on every court page — which is why it is a
// single indexed read on the reader's pool and returns nothing rather than an
// error when a court has no server. Most courts will not have one.
func (s *Store) Listed(ctx context.Context, chain, court string) (*Binding, error) {
	var b Binding
	var listedAt, checkedAt sql.NullInt64
	var signer, invite sql.NullString
	err := s.r.QueryRowContext(ctx,
		`SELECT chain, court, guild_id, state, signer, invite, bound_at, listed_at, checked_at
		   FROM guild_bindings WHERE chain = ? AND court = ? AND state = ?`,
		chain, court, StateListed).
		Scan(&b.Chain, &b.Court, &b.GuildID, &b.State, &signer, &invite,
			&boundAtScan{&b.BoundAt}, &listedAt, &checkedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.Signer, b.Invite = signer.String, invite.String
	if listedAt.Valid {
		b.ListedAt = time.Unix(listedAt.Int64, 0)
	}
	if checkedAt.Valid {
		b.CheckedAt = time.Unix(checkedAt.Int64, 0)
	}
	return &b, nil
}

// boundAtScan reads a unix seconds column into a time.Time.
type boundAtScan struct{ t *time.Time }

func (b *boundAtScan) Scan(v any) error {
	n, ok := v.(int64)
	if !ok {
		return fmt.Errorf("binding: bound_at was %T, not an integer", v)
	}
	*b.t = time.Unix(n, 0)
	return nil
}

// SetInvite records the link a reader clicks.
//
// SEPARATE FROM List BECAUSE IT CAN FAIL ON ITS OWN. Minting an invite is a call
// to Discord, and a guild that granted the publish but withheld
// CREATE_INSTANT_INVITE is a real state — the court's moderators chose the
// server, and the bot cannot yet produce a door to it. Folding this into List
// would mean either failing the publish over a link, or pretending the publish
// happened when the row says nothing a reader can use.
func (s *Store) SetInvite(ctx context.Context, chain, court, guildID, invite string) error {
	// COURT-KEYED LIKE List, not guild-keyed. It is reached only after List has
	// matched the court, so today the extra term changes nothing — which is
	// exactly why it is here: the two bugs this package has had were both a
	// statement keyed on the guild when the court is what authorises it, and
	// "the caller checked" is what made both of them invisible.
	res, err := s.w.ExecContext(ctx,
		`UPDATE guild_bindings SET invite = ?
		  WHERE chain = ? AND guild_id = ? AND court = ?`,
		invite, chain, guildID, court)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoBinding
	}
	return nil
}

// SetState moves a binding and records why, which is the whole audit trail a
// delisted server gets.
func (s *Store) SetState(ctx context.Context, chain, guildID, state, note string) error {
	res, err := s.w.ExecContext(ctx,
		`UPDATE guild_bindings SET state = ?, note = ?, checked_at = ?
		  WHERE chain = ? AND guild_id = ?`,
		state, note, s.now().Unix(), chain, guildID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoBinding
	}
	return nil
}

// Watched returns the bindings a heartbeat has to look at: the published ones and
// the ones that were published and are currently failing.
//
// Delisted rows are NOT returned. They are over, and continuing to poll them is
// how a sweep's cost grows without bound against a shared request budget — and
// against Discord's 10,000-4xx-in-ten-minutes IP ban, since a delisted guild is
// exactly the kind the bot is no longer in.
func (s *Store) Watched(ctx context.Context) ([]Binding, error) {
	rows, err := s.r.QueryContext(ctx,
		`SELECT chain, court, guild_id, state, signer, invite, bound_at, checked_at
		   FROM guild_bindings WHERE state IN (?,?)`, StateListed, StateDegraded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Binding
	for rows.Next() {
		var b Binding
		var signer, invite sql.NullString
		var boundAt int64
		var checkedAt sql.NullInt64
		if err := rows.Scan(&b.Chain, &b.Court, &b.GuildID, &b.State,
			&signer, &invite, &boundAt, &checkedAt); err != nil {
			return nil, err
		}
		b.Signer, b.Invite, b.BoundAt = signer.String, invite.String, time.Unix(boundAt, 0)
		if checkedAt.Valid {
			b.CheckedAt = time.Unix(checkedAt.Int64, 0)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Degrade moves a listed binding to degraded, recording why and when it started.
//
// IT DOES NOT RESET THE CLOCK ON A BINDING ALREADY DEGRADED. The grace period is
// measured from the FIRST failure, and a sweep that re-degraded an already-failing
// row every few minutes would push the deadline forward forever — a court whose
// server has been broken for a week would never reach the end of its grace.
func (s *Store) Degrade(ctx context.Context, chain, guildID, note string) error {
	// ONE STATEMENT. This was an UPDATE followed by a conditional second UPDATE
	// to refresh only the note on an already-degraded row; the CASE does the same
	// thing in one round trip, and the branch it removes was the one place the
	// "clock runs from the first failure" rule could have been broken by editing
	// the wrong arm.
	_, err := s.w.ExecContext(ctx,
		`UPDATE guild_bindings
		    SET state = ?,
		        note  = ?,
		        checked_at = CASE WHEN state = ? THEN ? ELSE checked_at END
		  WHERE chain = ? AND guild_id = ? AND state IN (?, ?)`,
		StateDegraded, note, StateListed, s.now().Unix(),
		chain, guildID, StateListed, StateDegraded)
	return err
}

// Restore puts a degraded binding back, which is the ordinary end of a transient
// failure and must not need a human.
func (s *Store) Restore(ctx context.Context, chain, guildID string) error {
	_, err := s.w.ExecContext(ctx,
		`UPDATE guild_bindings SET state = ?, note = NULL, checked_at = ?
		  WHERE chain = ? AND guild_id = ? AND state = ?`,
		StateListed, s.now().Unix(), chain, guildID, StateDegraded)
	return err
}

// Touch records that a healthy binding was checked, so an operator can tell a
// sweep that is running from one that has quietly stopped.
func (s *Store) Touch(ctx context.Context, chain, guildID string) error {
	_, err := s.w.ExecContext(ctx,
		`UPDATE guild_bindings SET checked_at = ? WHERE chain = ? AND guild_id = ?`,
		s.now().Unix(), chain, guildID)
	return err
}

// Unlisted returns guilds the bot is in that have never been listed and are older
// than age — the ones it should leave.
//
// It deliberately does NOT return degraded or delisted rows. Those were listed
// once, so their guild has provisioned channels and a history, and leaving one
// because a mod election ran long would destroy something a court chose.
func (s *Store) Unlisted(ctx context.Context, age time.Duration) ([]Binding, error) {
	cutoff := s.now().Add(-age).Unix()
	rows, err := s.r.QueryContext(ctx,
		`SELECT chain, court, guild_id, bound_at FROM guild_bindings
		  WHERE state = ? AND bound_at < ?`, StatePending, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Binding
	for rows.Next() {
		var b Binding
		var boundAt int64
		if err := rows.Scan(&b.Chain, &b.Court, &b.GuildID, &boundAt); err != nil {
			return nil, err
		}
		b.State, b.BoundAt = StatePending, time.Unix(boundAt, 0)
		out = append(out, b)
	}
	return out, rows.Err()
}
