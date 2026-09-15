package guild

import (
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// THE STATE VALUE IS THE ONLY THING THAT SURVIVES THE ROUND TRIP.
//
// A reader leaves kourt.xyz for Discord's consent screen and comes back to
// /api/guild/bound with whatever Discord hands over. Discord hands over a `code`,
// a `guild_id` its own documentation calls a HINT, and this string. So this string
// is what has to say which court the flow was for — the guild id cannot be trusted
// to say it, and there is no session in the redirect beyond what we put here.
//
// Grammar: v1:<chain>:<court>:<nonce>
//
// THE NONCE IS SERVER-MINTED, ALWAYS. It is not a formatting detail and not a
// checksum: it is the only thing tying a returning redirect to a browser that
// actually started the flow. A client-chosen nonce verifies nothing — an attacker
// composes their own and it validates perfectly. The service mints it, stores it
// against a short-lived session, and redeems it exactly once. This package cannot
// enforce any of that, which is why Parse deliberately does not accept a state
// with an empty nonce: the shape at least makes the omission impossible to ship
// by accident.
//
// WHY A GRAMMAR RATHER THAN JSON. Discord echoes state as a query parameter and
// there is a length budget; more to the point, a hand-readable form is one an
// operator can eyeball in an nginx log while diagnosing a failed bind. The parts
// are colon-separated and no part may contain a colon, which the charset rules
// below make true by construction rather than by escaping.
const stateVersion = "v1"

// courtRe is the CHAIN's rule, not a convenience.
//
// `realm/r/kourtv2/court.gno:1193` (mustSlug) accepts 1..11 of [a-z0-9] and
// NOTHING else — the hyphen was deliberately dropped there because a court's coin
// symbol is its upper-cased slug and KOURT:MY-COURT reads the hyphen as a minus.
//
// This is NARROWER than the two court patterns already in this tree —
// internal/chat/server.go:190 allows [a-z0-9-]{1,32} and internal/archive/server.go:23
// allows [a-z0-9-]{1,11}. Both are fine where they are: they guard a lookup that
// simply misses for a bad slug. This one is different, because the slug it accepts
// goes into a value that is later SIGNED, and a signature over a court that cannot
// exist is a signature nobody can reason about. Matching the chain exactly is the
// only defensible rule for that, and widening it later would be the unsafe
// direction.
var courtRe = regexp.MustCompile(`^[a-z0-9]{1,11}$`)

// ValidCourt is that rule, exported, because a second package needs it.
//
// internal/binding interpolates a court slug into a qeval expression, where the
// job is stopping the expression being reopened rather than keeping a signature
// well-formed — a different reason for the same constraint. It kept its own copy
// of this regex, which is the arrangement check-addr-shapes.py exists to police
// elsewhere and nothing was policing here: a change to the chain's mustSlug would
// have been made in whichever file the person was looking at.
//
// Exported rather than guarded because binding already imports this package, so
// sharing costs no dependency at all — the duplication was self-inflicted by the
// regex being unexported.
func ValidCourt(s string) bool { return courtRe.MatchString(s) }

// chainRe matches a gno chain id ("dev", "kourt-1"). Hyphens are ordinary here —
// a chain id is not a coin symbol.
//
// Also narrower than the tree's other one, for the same reason courtRe is:
// internal/chat/server.go allows [A-Za-z0-9._-]{1,32}, so uppercase and dots.
// This value goes into a signed payload, and two spellings of one chain that
// differ only in case would be two different signatures over what a reader would
// call the same thing.
var chainRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// nonceMinBytes is a floor on entropy, not on formatting.
//
// Sixteen bytes because the nonce is a single-use bearer value in a URL: below
// that, guessing an outstanding one stops being obviously hopeless, and the cost
// of more entropy is a handful of characters in a link nobody types by hand.
const nonceMinBytes = 16

var (
	ErrStateShape   = errors.New("guild: state is not v1:<chain>:<court>:<nonce>")
	ErrStateChain   = errors.New("guild: state names an impossible chain id")
	ErrStateCourt   = errors.New("guild: state names an impossible court slug")
	ErrStateNonce   = errors.New("guild: state nonce is not lowercase hex of at least 16 bytes")
	ErrStateVersion = errors.New("guild: state is not version v1")
)

// State is one OAuth round trip: which chain, which court, and the single-use
// value proving the redirect belongs to a flow this service started.
type State struct {
	Chain string
	Court string
	Nonce string // lowercase hex, server-minted, >= nonceMinBytes of entropy
}

// FormatState renders a State for the authorize URL's state parameter.
//
// It VALIDATES rather than trusting its caller. The caller is our own handler, so
// this looks redundant — but the one thing worse than rejecting a bad bind is
// minting a link whose state cannot be parsed on the way back, stranding a real
// moderator at a redirect that errors with nothing to retry. Failing here fails
// before the reader has left the page.
func FormatState(s State) (string, error) {
	if err := validate(s); err != nil {
		return "", err
	}
	return strings.Join([]string{stateVersion, s.Chain, s.Court, s.Nonce}, ":"), nil
}

// validate is the single spelling of what a usable State is.
//
// It exists because FormatState and ParseState ran the same three checks in the
// same order, differing only in the zero value they return — which is the shape
// where somebody adds a fourth check to one side and the two quietly disagree
// about what is acceptable. Writing on one side and reading on the other is
// exactly where that disagreement would be invisible.
func validate(s State) error {
	if !chainRe.MatchString(s.Chain) {
		return fmt.Errorf("%w: %q", ErrStateChain, s.Chain)
	}
	if !courtRe.MatchString(s.Court) {
		return fmt.Errorf("%w: %q", ErrStateCourt, s.Court)
	}
	return checkNonce(s.Nonce)
}

// ParseState reads back what FormatState wrote.
//
// Everything it returns is attacker-supplied — Discord echoes whatever was in the
// link, and anyone can compose a link. So a parse that succeeds means only "this
// is well-formed and names a court that could exist"; it does NOT mean the nonce
// was ever issued, and it emphatically does not mean the person is a moderator.
// Both of those are the service's checks, against its own store and against the
// chain. This function's job is to make sure nothing downstream has to think about
// a colon in a court slug.
func ParseState(raw string) (State, error) {
	parts := strings.Split(raw, ":")
	if len(parts) != 4 {
		return State{}, fmt.Errorf("%w: got %d part(s)", ErrStateShape, len(parts))
	}
	if parts[0] != stateVersion {
		return State{}, fmt.Errorf("%w: %q", ErrStateVersion, parts[0])
	}
	s := State{Chain: parts[1], Court: parts[2], Nonce: parts[3]}
	if err := validate(s); err != nil {
		return State{}, err
	}
	return s, nil
}

func checkNonce(n string) error {
	if n != strings.ToLower(n) {
		return fmt.Errorf("%w: not lowercase", ErrStateNonce)
	}
	b, err := hex.DecodeString(n)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrStateNonce, err)
	}
	if len(b) < nonceMinBytes {
		return fmt.Errorf("%w: %d bytes", ErrStateNonce, len(b))
	}
	return nil
}

// ChallengeText is what a court moderator signs to claim the publish slot.
//
// IT BINDS THREE THINGS AND ALL THREE ARE LOAD-BEARING:
//
//	court     — so a signature for one court cannot list a server under another
//	guild id  — so a leaked signature cannot be replayed against a second guild,
//	            which is the whole attack: a moderator's signature pasted into a
//	            support channel or screenshotted is otherwise a reusable warrant
//	nonce     — so it is single-use and expires with the session that minted it
//
// The earlier draft of this design said only "the moderator signs a challenge",
// and a challenge missing the guild id is a signature that lists any server the
// holder likes. Spelling the payload out in one function, used by both the minting
// side and the verifying side, is what stops the two drifting apart.
//
// The text is human-readable because a signing prompt shows it to somebody who is
// about to authorise something consequential, and a wall of base64 trains people
// to click through.
func ChallengeText(s State, guildID string) (string, error) {
	if _, err := FormatState(s); err != nil {
		return "", err
	}
	if !guildIDRe.MatchString(guildID) {
		return "", fmt.Errorf("guild: %q is not a Discord snowflake", guildID)
	}
	return fmt.Sprintf(
		"kourt: list Discord guild %s as the server for court %q on chain %q\nnonce %s",
		guildID, s.Court, s.Chain, s.Nonce), nil
}

// guildIDRe matches a Discord snowflake: decimal, and long enough to be one.
//
// Snowflakes are 64-bit ids rendered as decimal strings. They are not validated
// for range here because Discord is the authority on which ones exist; this only
// rejects the shapes that are certainly not ids, so they never reach a signed
// payload or a log line.
var guildIDRe = regexp.MustCompile(`^[0-9]{17,20}$`)
