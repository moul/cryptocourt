package binding

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"strings"
	"time"

	"github.com/jaekwon/kourt/internal/guild"
)

// THE HEARTBEAT IS THE ONLY THING PROTECTING A LISTING AFTER IT IS PUBLISHED.
//
// Everything up to /claim decides whether a court's moderators chose a server.
// Nothing in that moment says the choice is still true an hour later: the bot can
// be kicked, its permissions stripped, or the moderator who signed can be removed
// from the court's set on chain. This sweep is what notices, and what takes the
// link off the court page when it stops being true.
//
// THE FAIL DIRECTION IS THE OPPOSITE OF /claim's, AND GETTING IT BACKWARDS WOULD
// BE THE WORST BUG IN THIS FILE.
//
//	granting  — an answer nobody got must behave like NO. Do not publish on silence.
//	keeping   — an answer nobody got must NOT behave like no. A node blip or a
//	            Discord outage would otherwise delist every court on the site at
//	            once, an outage of ours triggered by somebody else's.
//
// So a failure to REACH Discord or the chain aborts the sweep without touching a
// single row, and only a definite negative answer degrades anything.
type Heartbeat struct {
	Store    *Store
	Verifier *Verifier
	Discord  Fleet
	Log      *log.Logger

	// Grace is how long a degraded listing is kept before it is delisted. It is
	// generous on purpose: the usual cause is transient, and a moderator election
	// running long must not cost a court the server it already provisioned.
	Grace time.Duration

	now func() time.Time
}

// DefaultGrace is a week.
//
// NOT HOURS. The failures this waits through are a permission an owner will
// restore when told, and a mod-set change on chain that governance is in the
// middle of. Both are measured in days, and the cost of waiting is a hidden link
// rather than a wrong one — the listing is already invisible the moment it
// degrades. Delisting exists to stop dead rows accumulating, not to punish.
const DefaultGrace = 7 * 24 * time.Hour

func (h *Heartbeat) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

// SetClock replaces the clock, for tests.
func (h *Heartbeat) SetClock(f func() time.Time) { h.now = f }

func (h *Heartbeat) logf(format string, args ...any) {
	if h.Log != nil {
		h.Log.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Result is what one sweep did, for a log line and for a test.
type Result struct {
	Checked  int
	Degraded int
	Restored int
	Delisted int
}

// Once runs a single sweep.
//
// IT READS DISCORD EXACTLY ONCE for the whole fleet. /users/@me/guilds pages 200
// at a time and carries the bot's permissions per guild, so a hundred listed
// courts cost one request rather than a hundred against a budget of 50 per second
// shared with everything else — and a kicked bot appears as an ABSENCE rather than
// a 403, which keeps the sweep away from the 10,000-4xx-in-ten-minutes IP ban.
func (h *Heartbeat) Once(ctx context.Context) (Result, error) {
	var res Result

	watched, err := h.Store.Watched(ctx)
	if err != nil {
		return res, err
	}
	if len(watched) == 0 {
		return res, nil
	}

	// ONE READ, BEFORE ANY DECISION. If this fails, nothing is degraded: the
	// whole fleet looking absent is what a Discord outage looks like, and acting
	// on it would delist every court at once.
	present, err := h.Discord.MyGuilds(ctx)
	if err != nil {
		return res, fmt.Errorf("heartbeat: not reaching Discord, so nothing was "+
			"changed: %w", err)
	}

	grace := h.Grace
	if grace <= 0 {
		grace = DefaultGrace
	}

	for _, b := range watched {
		res.Checked++
		why := h.trouble(ctx, b, present)

		switch {
		case why == unknown:
			// The chain did not answer about this one. Leave it exactly as it is
			// — including leaving a degraded row degraded, whose grace clock is
			// measured from its first failure and so keeps running.
			h.logf("guild: %s/%s could not be checked; left %s", b.Chain, b.Court, b.State)

		case why == "":
			if b.State == StateDegraded {
				if err := h.Store.Restore(ctx, b.Chain, b.GuildID); err != nil {
					h.logf("guild: restoring %s: %v", b.GuildID, err)
					continue
				}
				res.Restored++
				h.logf("guild: %s/%s is healthy again and is listed once more", b.Chain, b.Court)
				continue
			}
			if err := h.Store.Touch(ctx, b.Chain, b.GuildID); err != nil {
				h.logf("guild: touching %s: %v", b.GuildID, err)
			}

		default:
			// A DEGRADED ROW PAST ITS GRACE IS DELISTED, and the clock runs from
			// the first failure rather than from this sweep — see Store.Degrade,
			// which deliberately does not reset checked_at on a row already
			// degraded.
			if b.State == StateDegraded && !b.CheckedAt.IsZero() &&
				h.clock().Sub(b.CheckedAt) > grace {
				if err := h.Store.SetState(ctx, b.Chain, b.GuildID, StateDelisted,
					"delisted after "+grace.String()+" degraded: "+why); err != nil {
					h.logf("guild: delisting %s: %v", b.GuildID, err)
					continue
				}
				res.Delisted++
				h.logf("guild: %s/%s delisted after %s degraded — %s",
					b.Chain, b.Court, grace, why)
				continue
			}
			// THE WRITE FIRST, THEN THE COUNT AND THE LINE. These were the
			// other way round, so a transient database error left the row still
			// listed — still served to readers — while the sweep's own result
			// and log said it had been degraded. The Restored and Delisted
			// branches above already had this order; this one did not.
			if err := h.Store.Degrade(ctx, b.Chain, b.GuildID, why); err != nil {
				h.logf("guild: degrading %s: %v", b.GuildID, err)
				continue
			}
			if b.State == StateListed {
				res.Degraded++
				h.logf("guild: %s/%s degraded — %s", b.Chain, b.Court, why)
			}
		}
	}
	return res, nil
}

// unknown is the third answer, distinct from "fine" and from a reason.
const unknown = "\x00unknown"

// trouble reports why a binding should not be published, "" if it should, or
// unknown if this sweep could not tell.
//
// THE CHEAP CHECKS COME FIRST, and not only for speed. Presence and permissions
// are answered by data already in hand; only the moderator check costs a chain
// read, so a guild the bot was kicked from never spends one.
func (h *Heartbeat) trouble(ctx context.Context, b Binding, present map[string]guild.Permission) string {
	have, in := present[b.GuildID]
	if !in {
		return "the bot is no longer in that server"
	}
	if missing := guild.Missing(have); len(missing) > 0 {
		// Named, because this ends up in front of a server owner who has to
		// restore them, and a bitfield is a number somebody has to go and decode.
		return "the bot is missing " + strings.Join(missing, ", ")
	}
	if b.Signer == "" {
		return "no signer is recorded for this listing"
	}

	isMod, err := h.Verifier.IsMod(ctx, b.Court, b.Signer)
	if err != nil {
		// NOT A REFUSAL. See the type comment: an unreachable chain must not
		// delist. The one exception is a court that is definitely gone, which is
		// an answer rather than a silence.
		if errors.Is(err, ErrNoCourt) {
			return "that court no longer exists on this chain"
		}
		return unknown
	}
	if !isMod {
		return "the address that published this server no longer moderates the court"
	}
	return ""
}

// Fleet is the half of Discord the heartbeat needs: one read of every guild the
// bot is in, with the permissions it holds there. Named rather than inlined on
// the struct for the reason Leaver is — a dependency worth faking in a test is a
// dependency worth naming, and the two were inconsistent.
type Fleet interface {
	MyGuilds(ctx context.Context) (map[string]guild.Permission, error)
}

// Leaver is the half of Discord the auto-leave needs. Separate from the fleet
// read so a caller can supply one without the other, and so a test can assert
// what was left without standing up a REST client.
type Leaver interface {
	Leave(guildID string) error
}

// LeaveAfter is how long a guild the bot was added to may sit unclaimed.
//
// The bot can be installed into any guild by anybody — client id and permissions
// are in the page source by necessity — so presence proves nothing and grants
// nothing. What it still costs is standing exposure: the bot can enumerate that
// guild's structure for as long as it is in it. A day is long enough for somebody
// to finish a flow they started and short enough that the fleet is mostly guilds
// that meant something.
const LeaveAfter = 24 * time.Hour

// Sweep runs the checks that are not per-listing: leaving guilds nobody claimed,
// and clearing spent flows.
//
// NOTHING HERE TOUCHES A ROW THAT WAS EVER LISTED. Store.Unlisted returns pending
// rows only — never degraded or delisted — because those guilds have provisioned
// channels and a history, and leaving one because a moderator election ran long
// would destroy something a court chose.
func (h *Heartbeat) Sweep(ctx context.Context, leaver Leaver) (left int, err error) {
	if n, err := h.Store.SweepFlows(ctx); err != nil {
		h.logf("guild: sweeping spent flows: %v", err)
	} else if n > 0 {
		h.logf("guild: cleared %d expired flow(s)", n)
	}
	if leaver == nil {
		return 0, nil
	}
	stale, err := h.Store.Unlisted(ctx, LeaveAfter)
	if err != nil {
		return 0, err
	}
	for _, b := range stale {
		if err := leaver.Leave(b.GuildID); err != nil {
			// Not fatal and not retried hard: a guild that is already gone
			// answers 404, and treating that as a failure would mean trying
			// forever. The row is closed either way, which is what stops the
			// next sweep looking at it again.
			h.logf("guild: leaving %s: %v", b.GuildID, err)
		}
		if err := h.Store.SetState(ctx, b.Chain, b.GuildID, StateDelisted,
			"left after "+LeaveAfter.String()+" unclaimed"); err != nil {
			h.logf("guild: closing %s: %v", b.GuildID, err)
			continue
		}
		left++
	}
	if left > 0 {
		h.logf("guild: left %d guild(s) nobody claimed", left)
	}
	return left, nil
}

// Run sweeps on a jittered interval until ctx is done.
//
// THE CADENCE IS JITTERED, WHICH IS NOT COSMETIC. A fixed period is a window an
// owner can work inside: strip the bot's permissions just after a sweep, do
// whatever the permissions were stopping, and restore them before the next one,
// and every check the site ever makes says the server is healthy. Jitter does not
// close that window — nothing polling can — but it stops it being schedulable.
//
// IT ALSO NEVER RUNS TWO SWEEPS AT ONCE. Each iteration waits for the last to
// finish before timing the next, so a slow Discord makes sweeps less frequent
// rather than piling them up against a shared request budget.
func (h *Heartbeat) Run(ctx context.Context, every time.Duration, leaver Leaver) {
	if every <= 0 {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter(every)):
		}
		if r, err := h.Once(ctx); err != nil {
			h.logf("guild: heartbeat: %v", err)
		} else if r.Degraded+r.Restored+r.Delisted > 0 {
			h.logf("guild: heartbeat checked %d, degraded %d, restored %d, delisted %d",
				r.Checked, r.Degraded, r.Restored, r.Delisted)
		}
		if _, err := h.Sweep(ctx, leaver); err != nil {
			h.logf("guild: sweep: %v", err)
		}
	}
}

// jitter returns d plus or minus up to a quarter of it.
//
// A QUARTER, NOT A COIN FLIP BETWEEN TWO VALUES: the point is that the next sweep
// is not predictable from the last, and a spread narrow enough to keep the
// average cadence honest is enough for that.
func jitter(d time.Duration) time.Duration {
	half := int64(d / 2)
	if half <= 0 {
		return d
	}
	n, err := rand.Int(rand.Reader, big.NewInt(half))
	if err != nil {
		return d
	}
	return d - time.Duration(half/2) + time.Duration(n.Int64())
}
