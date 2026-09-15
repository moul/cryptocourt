// Package binding decides which Discord guild, if any, kourt.xyz publishes for a
// court — and answers the one question that decision rests on: is this address a
// moderator of this court, according to the chain?
//
// It is the impure counterpart to internal/guild. That package holds the rules
// that can be checked without a network (the permission set, the state grammar,
// the signed challenge); this one holds the parts that must ask somebody else.
package binding

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jaekwon/kourt/internal/gnorpc"
	"github.com/jaekwon/kourt/internal/guild"
)

// DefaultPkgPath is the realm on kourt-1.
const DefaultPkgPath = "gno.land/r/kourt/kourtv2"

// addrRe is a gno bech32 account address.
//
// THIS IS AN INJECTION GUARD, not a politeness. The address is interpolated into
// a qeval expression — `IsCourtMod("meta","g1…")` — so an address containing a
// quote could close the string and append another argument, or another call. It
// is the only caller-supplied value that reaches the expression besides the court
// slug, which internal/guild already constrains to [a-z0-9]{1,11}.
//
// The charset is bech32's: lowercase, and without the four characters bech32
// excludes because they misread — 1, b, i and o — except that the "g1" prefix
// contributes its own 1.
var addrRe = regexp.MustCompile(`^g1[023456789acdefghjklmnpqrstuvwxyz]{38}$`)

// THE COURT RULE COMES FROM internal/guild, not from a copy here.
//
// THIS PACKAGE USED TO KEEP ITS OWN COPY. The reason given was that the other one
// was unexported — self-inflicted, since binding already imports guild — and the
// cost was two byte-identical regexes with the same "must track the chain
// exactly" justification and nothing tying them together. Here the constraint
// stops an interpolated qeval expression being reopened; there it keeps a signed
// payload well-formed. Same rule, two reasons, one definition.
// (No local variable: callers say guild.ValidCourt, which names where the rule
// comes from at every site that applies it.)

var (
	ErrBadAddress = errors.New("binding: not a gno address")
	ErrBadCourt   = errors.New("binding: not a court slug")
	// ErrUnreadable is what a caller must distinguish from "no".
	ErrUnreadable = errors.New("binding: the chain's answer could not be read")
	// ErrNoCourt is a court the chain has never heard of, which is a different
	// thing from a court that said no.
	ErrNoCourt = errors.New("binding: no such court on this chain")
)

// noSuchCourt is how the realm's panic reaches us: mustCourt panics with this
// text (realm/r/kourtv2/court.gno), and the node wraps it in a formatted
// traceback several lines long. Matching the sentence rather than the whole
// envelope is the durable half — the traceback's shape is the node's business and
// has changed before, the panic text is the realm's and is asserted by its tests.
const noSuchCourt = "kourtv2: no such court"

// classify turns a node error into one a caller can branch on, keeping the
// original underneath so nothing is lost for a log.
func classify(court string, err error) error {
	if err != nil && strings.Contains(err.Error(), noSuchCourt) {
		return fmt.Errorf("%w: %q", ErrNoCourt, court)
	}
	return err
}

// Verifier answers moderator questions against a node.
type Verifier struct {
	Node    *gnorpc.Node
	PkgPath string // "" means DefaultPkgPath
}

func (v *Verifier) pkg() string {
	if v.PkgPath == "" {
		return DefaultPkgPath
	}
	return v.PkgPath
}

// IsMod reports whether the chain considers addr a moderator of court.
//
// A FALSE AND AN ERROR ARE DIFFERENT ANSWERS AND CALLERS MUST TREAT THEM SO.
// False means the chain answered and said no. An error means nobody knows — the
// node was unreachable, or it said something this code cannot parse. The two have
// opposite safe handlings, which is why this never collapses them:
//
//	granting a listing   — an error must behave like false. Do not publish on an
//	                       answer nobody got.
//	keeping a listing     — an error must NOT behave like false. A node blip would
//	                       otherwise delist every court on the site at once, which
//	                       is a self-inflicted outage triggered by someone else's
//	                       infrastructure.
//
// The parse is deliberately strict. qeval prints `(true bool)`; anything else is
// ErrUnreadable rather than a shrug that happens to equal "not a moderator",
// because "unparseable therefore no" is how a realm upgrade silently revokes
// every court's Discord.
func (v *Verifier) IsMod(ctx context.Context, court, addr string) (bool, error) {
	if !guild.ValidCourt(court) {
		return false, fmt.Errorf("%w: %q", ErrBadCourt, court)
	}
	if !addrRe.MatchString(addr) {
		return false, fmt.Errorf("%w: %q", ErrBadAddress, addr)
	}
	out, err := v.Node.QEval(ctx, v.pkg(), fmt.Sprintf("IsCourtMod(%q,%q)", court, addr))
	if err != nil {
		return false, classify(court, err)
	}
	return parseBool(out)
}

// ModThreshold is the court's m-of-n, as the chain reports it.
//
// It is read but not yet enforced: every other set-level moderator act in the
// realm goes through m-of-n approval, and a publish that takes one signature lets
// a single minority moderator displace the majority's server at will. Reading it
// now means the number is available the day that is fixed, and means a court's
// page can already state what its threshold is.
func (v *Verifier) ModThreshold(ctx context.Context, court string) (m, n int, err error) {
	if !guild.ValidCourt(court) {
		return 0, 0, fmt.Errorf("%w: %q", ErrBadCourt, court)
	}
	out, err := v.Node.QEval(ctx, v.pkg(), fmt.Sprintf("ModThreshold(%q)", court))
	if err != nil {
		return 0, 0, classify(court, err)
	}
	// Two return values print as two parenthesised terms: `(1 int)(1 int)`.
	fields := splitTerms(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("%w: ModThreshold said %q", ErrUnreadable, out)
	}
	if _, err := fmt.Sscanf(fields[0], "%d", &m); err != nil {
		return 0, 0, fmt.Errorf("%w: m was %q", ErrUnreadable, fields[0])
	}
	if _, err := fmt.Sscanf(fields[1], "%d", &n); err != nil {
		return 0, 0, fmt.Errorf("%w: n was %q", ErrUnreadable, fields[1])
	}
	if m < 1 || n < 1 || m > n {
		return 0, 0, fmt.Errorf("%w: ModThreshold said %d-of-%d", ErrUnreadable, m, n)
	}
	return m, n, nil
}

func parseBool(out string) (bool, error) {
	switch strings.TrimSpace(out) {
	case "(true bool)":
		return true, nil
	case "(false bool)":
		return false, nil
	}
	return false, fmt.Errorf("%w: expected a bool, got %q", ErrUnreadable, out)
}

// splitTerms breaks `(1 int)(2 int)` into ["1 int", "2 int"].
func splitTerms(out string) []string {
	var terms []string
	for _, part := range strings.Split(strings.TrimSpace(out), ")") {
		part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "("))
		if part != "" {
			terms = append(terms, part)
		}
	}
	return terms
}
