package guild

import (
	"fmt"
	"strings"
)

// THE LAYOUT A COURT'S SERVER GETS, described once, in code.
//
// It is a DESCRIPTOR AND A PLANNER, not a client. Nothing here performs a request:
// CourtLayout() states the shape and Plan() turns it into an ordered list of the
// calls that would build it. The command that actually talks to Discord executes
// that list, and prints it instead when it has not been told to apply.
//
// Doing it this way is what makes a layout reviewable. The alternative — a
// function that walks the structure issuing HTTP as it goes — can only be checked
// against a live guild, so in practice it is never checked at all, and a field
// somebody added to the struct and forgot to send looks identical to one that
// works. Here the test asserts every field of the descriptor is reached by the
// planner, which is a property a unit test can actually hold.

// ChannelType mirrors Discord's channel type numbers.
type ChannelType int

const (
	ChannelText     ChannelType = 0
	ChannelCategory ChannelType = 4
	ChannelForum    ChannelType = 15
)

func (c ChannelType) String() string {
	switch c {
	case ChannelText:
		return "text"
	case ChannelCategory:
		return "category"
	case ChannelForum:
		return "forum"
	}
	return fmt.Sprintf("type(%d)", int(c))
}

// EveryoneRole is the name the planner uses for the guild's default role.
//
// Discord's @everyone role id equals the guild id, which the planner does not
// know at plan time, so overwrites name it by this sentinel and the executor
// substitutes. Naming it rather than leaving it implicit is the point: an
// @everyone overwrite is the difference between a court server and an open mic.
const EveryoneRole = "@everyone"

// Role is one role the layout creates.
type Role struct {
	Name string
	// Permissions is the role's guild-wide grant. Zero is meaningful and common:
	// a marker role that grants nothing and exists only to be mentioned or to
	// carry a channel overwrite.
	Permissions Permission
	Hoist       bool // shown separately in the member list
	Mentionable bool
	Why         string
}

// Overwrite is a per-channel permission exception.
type Overwrite struct {
	Role  string // a Role.Name, or EveryoneRole
	Allow Permission
	Deny  Permission
}

// Channel is one channel or category.
type Channel struct {
	Name       string
	Type       ChannelType
	Topic      string
	Parent     string // a Channel.Name of Type ChannelCategory, or "" for top level
	Overwrites []Overwrite
	// Tags are forum tags. Discord caps available_tags at 20 per forum and
	// applied_tags at 5 per thread; the planner enforces the former because
	// exceeding it fails the whole channel creation rather than truncating.
	Tags []string
}

// Layout is the whole server.
type Layout struct {
	Roles    []Role
	Channels []Channel
}

// maxForumTags is Discord's documented available_tags ceiling.
const maxForumTags = 20

// maxCategoryChildren is Discord's per-category child ceiling.
//
// ENFORCED, not merely recorded. It sat here unchecked next to maxForumTags,
// which Plan does enforce, and an unenforced limit beside an enforced one reads
// as coverage that is not there. The court layout is nowhere near 50 — but the
// obvious future edit is a channel per claim, which is exactly what would cross
// it, and the failure lands on a live guild rather than in a test.
const maxCategoryChildren = 50

// CourtLayout is the reference server for one court.
//
// SMALL ON PURPOSE. Every channel here is one somebody has a reason to open on
// the first day; a template is easy to add to later and very awkward to prune,
// because pruning means telling existing servers to delete things.
//
// The claim threads live in a FORUM rather than a channel each. Threads are
// exempt from the 500-channel ceiling and channels are not, so a court that runs
// for years stays inside one structure instead of eventually needing a second
// server. It also gives the docket a search box for free.
func CourtLayout() Layout {
	// The clerk's own role carries nothing. Its powers arrive with the bot's
	// managed role at invite time; this exists so a reader can see at a glance
	// who is speaking officially, and so channel overwrites have something to
	// name that is not the bot's own managed role — which the bot cannot
	// reposition and should not depend on for hierarchy.
	return Layout{
		Roles: []Role{
			{
				Name: "clerk", Hoist: true, Mentionable: false,
				Why: "marks the bot's messages as the court's own, visibly, in the member list",
			},
			{
				Name: "verified party", Hoist: true, Mentionable: true,
				Why: "an address this court recognises: author, answerer, or moderator",
			},
		},
		Channels: []Channel{
			{
				Name: "the court", Type: ChannelCategory,
				// READ-ONLY BY DEFAULT, and this is the load-bearing overwrite in
				// the whole layout. Without it the docket is a channel anybody can
				// post into, and a mirrored verdict sits in the same scroll as
				// whatever a stranger typed after it — which is precisely the
				// confusion a court server exists to avoid.
				Overwrites: []Overwrite{
					{Role: EveryoneRole, Deny: PermSendMessages | PermCreatePublicThreads},
					{Role: "clerk", Allow: PermSendMessages | PermCreatePublicThreads | PermManageThreads},
				},
			},
			{
				Name: "docket", Type: ChannelText, Parent: "the court",
				Topic: "Claims as they are filed, and verdicts as they land. Posted by the clerk; mirrored from the chain.",
			},
			{
				Name: "how-this-works", Type: ChannelText, Parent: "the court",
				Topic: "What this server is, what the clerk does, and what being listed on kourt.xyz does and does not mean.",
			},
			{
				Name: "claims", Type: ChannelForum, Parent: "the court",
				Topic: "One post per claim. Argue inside the claim's own thread.",
				// Tags mirror the phases the overlay already filters by, so a
				// reader who knows the site knows this list.
				Tags: []string{"open", "answered", "disputed", "provisional", "settled", "void"},
				// The forum is where readers MAY write — the category denies it,
				// so the exception is stated here rather than assumed.
				Overwrites: []Overwrite{
					{Role: EveryoneRole, Allow: PermSendMessagesInThread | PermCreatePublicThreads},
				},
			},
			{
				Name: "lobby", Type: ChannelText,
				Topic: "Everything else. Not the court record.",
			},
		},
	}
}

// Op is one call the executor makes, in order.
//
// Fields is a rendered, sorted description rather than a request body because
// this type's first job is to be READ — in dry-run output, in a test failure, in
// a diff between two versions of the layout. The executor builds the real body
// from the same descriptor.
type Op struct {
	Kind   string // "role" or "channel"
	Name   string
	Fields []string
}

func (o Op) String() string {
	return fmt.Sprintf("create %-8s %-16q %s", o.Kind, o.Name, strings.Join(o.Fields, "  "))
}

// Ordered is the sequence channels must be created in, and it is EXPORTED
// because the executor needs the same answer.
//
// Categories first, then everything else, each preserving declaration order so
// the layout reads top to bottom the way the server will. Getting this wrong does
// not fail loudly — Discord accepts a channel naming a parent that does not exist
// yet by ignoring the parent, so the category ends up empty and everything sits at
// the top level looking like a styling problem.
//
// It lives here rather than in the command because the planner and the executor
// were each deriving it separately, in different shapes, from the same Layout.
// Two implementations of one silent invariant is the arrangement where a fix to
// either is a fix to neither.
func Ordered(chs []Channel) []Channel {
	out := make([]Channel, 0, len(chs))
	for _, c := range chs {
		if c.Type == ChannelCategory {
			out = append(out, c)
		}
	}
	for _, c := range chs {
		if c.Type != ChannelCategory {
			out = append(out, c)
		}
	}
	return out
}

// Plan turns a Layout into the ordered calls that build it.
//
// ORDER MATTERS AND IS NOT ALPHABETICAL. Roles come first because channel
// overwrites name them, and categories come before their children because a
// child sends its parent's id. Getting this wrong does not fail loudly — Discord
// accepts a channel with a parent that does not exist yet by ignoring the parent
// — so the ordering is asserted by a test rather than left to the reader.
//
// It also validates, and returns every problem rather than the first: a layout is
// edited as a whole and fixing one error at a time through a round trip to a live
// guild is how people give up and start clicking.
func Plan(l Layout) ([]Op, []error) {
	var ops []Op
	var errs []error

	roles := map[string]bool{EveryoneRole: true}
	for _, r := range l.Roles {
		if r.Name == "" {
			errs = append(errs, fmt.Errorf("a role has no name"))
			continue
		}
		if roles[r.Name] {
			errs = append(errs, fmt.Errorf("role %q is declared twice", r.Name))
		}
		roles[r.Name] = true
		f := []string{"perms=" + r.Permissions.String()}
		if r.Hoist {
			f = append(f, "hoist")
		}
		if r.Mentionable {
			f = append(f, "mentionable")
		}
		if r.Why == "" {
			errs = append(errs, fmt.Errorf("role %q has no Why; a role nobody can justify is a role to drop", r.Name))
		}
		ops = append(ops, Op{Kind: "role", Name: r.Name, Fields: f})
	}

	cats := map[string]bool{}
	children := map[string]int{}
	for _, c := range l.Channels {
		if c.Type == ChannelCategory {
			cats[c.Name] = true
		}
	}

	seen := map[string]bool{}
	for _, c := range Ordered(l.Channels) {
		if c.Name == "" {
			errs = append(errs, fmt.Errorf("a channel has no name"))
			continue
		}
		if seen[c.Name] {
			errs = append(errs, fmt.Errorf("channel %q is declared twice", c.Name))
		}
		seen[c.Name] = true

		f := []string{"type=" + c.Type.String()}
		if c.Parent != "" {
			if !cats[c.Parent] {
				errs = append(errs, fmt.Errorf("channel %q names parent %q, which is not a category in this layout", c.Name, c.Parent))
			}
			children[c.Parent]++
			if children[c.Parent] > maxCategoryChildren {
				errs = append(errs, fmt.Errorf("category %q holds %d channels; Discord allows %d",
					c.Parent, children[c.Parent], maxCategoryChildren))
			}
			f = append(f, fmt.Sprintf("parent=%q", c.Parent))
		}
		if c.Topic != "" {
			f = append(f, fmt.Sprintf("topic=%dch", len(c.Topic)))
		}
		if len(c.Tags) > 0 {
			if c.Type != ChannelForum {
				errs = append(errs, fmt.Errorf("channel %q is a %s but carries forum tags", c.Name, c.Type))
			}
			if len(c.Tags) > maxForumTags {
				errs = append(errs, fmt.Errorf("channel %q declares %d forum tags; Discord allows %d", c.Name, len(c.Tags), maxForumTags))
			}
			f = append(f, "tags="+strings.Join(c.Tags, ","))
		}
		for _, o := range c.Overwrites {
			if !roles[o.Role] {
				errs = append(errs, fmt.Errorf("channel %q has an overwrite for role %q, which this layout never creates", c.Name, o.Role))
			}
			if o.Allow&o.Deny != 0 {
				errs = append(errs, fmt.Errorf("channel %q allows and denies the same bits for %q", c.Name, o.Role))
			}
			f = append(f, fmt.Sprintf("overwrite[%s allow=%s deny=%s]", o.Role, o.Allow, o.Deny))
		}
		ops = append(ops, Op{Kind: "channel", Name: c.Name, Fields: f})
	}

	// Errors come back in DECLARATION ORDER, not sorted. They were sorted
	// alphabetically by message, which threw away the one ordering a person
	// editing this file already has in their head — the order the layout is
	// written in — and replaced it with an unrelated one. Nothing pinned the
	// sorted order; both callers just print each error on its own line.
	return ops, errs
}
