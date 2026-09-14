package guild

import (
	"reflect"
	"strings"
	"testing"
)

func planOf(t *testing.T, l Layout) string {
	t.Helper()
	ops, errs := Plan(l)
	for _, e := range errs {
		t.Errorf("unexpected layout error: %v", e)
	}
	var b strings.Builder
	for _, op := range ops {
		b.WriteString(op.String())
		b.WriteString("\n")
	}
	return b.String()
}

// A DESCRIPTOR FIELD NOTHING SENDS LOOKS EXACTLY LIKE ONE THAT WORKS.
//
// This package's only consumer is a command that talks to Discord, so there is no
// compiler error and no failing request when somebody adds a field to Channel and
// forgets the planner — the layout simply comes out subtly different from what the
// struct says, on a live guild, months later. That is the failure this test is
// for, and it is why Plan() exists as a pure function at all.
//
// It works by giving every field a value nothing else would produce and then
// looking for each one in the plan. The census below is the other half: add a
// field and the count check fails until this test covers it.
func TestEveryDescriptorFieldReachesThePlan(t *testing.T) {
	l := Layout{
		Roles: []Role{{
			Name:        "zzrole",
			Permissions: PermManageThreads,
			Hoist:       true,
			Mentionable: true,
			Why:         "so the role has a justification",
		}},
		Channels: []Channel{
			{Name: "zzcat", Type: ChannelCategory},
			{
				Name:   "zzforum",
				Type:   ChannelForum,
				Topic:  "zztopic",
				Parent: "zzcat",
				Tags:   []string{"zztag"},
				Overwrites: []Overwrite{
					{Role: "zzrole", Allow: PermSendMessages, Deny: PermAddReactions},
				},
			},
		},
	}
	got := planOf(t, l)

	for _, want := range []string{
		"zzrole",                             // Role.Name
		PermManageThreads.String(),           // Role.Permissions
		"hoist",                              // Role.Hoist
		"mentionable",                        // Role.Mentionable
		"zzcat",                              // Channel.Name, and Channel.Parent below
		"zzforum",                            //
		"type=forum",                         // Channel.Type
		"topic=7ch",                          // Channel.Topic
		`parent="zzcat"`,                     // Channel.Parent
		"tags=zztag",                         // Channel.Tags
		"overwrite[zzrole",                   // Overwrite.Role
		"allow=" + PermSendMessages.String(), // Overwrite.Allow
		"deny=" + PermAddReactions.String(),  // Overwrite.Deny
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the plan never mentions %q — a descriptor field is not being "+
				"sent.\nPlan:\n%s", want, got)
		}
	}

	// Role.Why is deliberately NOT in the plan: it justifies the role to a human
	// reading this file, and Discord has nowhere to put it. Asserted so that its
	// absence above reads as intended rather than as the bug this test hunts.
	if strings.Contains(got, "so the role has a justification") {
		t.Error("Role.Why reached the plan; it is documentation, not a field to send")
	}
}

// The census. reflect, so adding a field is a failing test rather than a silent
// gap in the check above.
func TestTheDescriptorHasNotGrownAFieldNothingChecks(t *testing.T) {
	for _, c := range []struct {
		name string
		typ  reflect.Type
		want int
	}{
		{"Layout", reflect.TypeOf(Layout{}), 2},
		{"Role", reflect.TypeOf(Role{}), 5},
		{"Channel", reflect.TypeOf(Channel{}), 6},
		{"Overwrite", reflect.TypeOf(Overwrite{}), 3},
	} {
		if got := c.typ.NumField(); got != c.want {
			t.Errorf("%s has %d fields, expected %d. If you added one, add it to "+
				"TestEveryDescriptorFieldReachesThePlan and to Plan() — or state here "+
				"why it is documentation rather than something to send.",
				c.name, got, c.want)
		}
	}
}

// ORDER IS NOT ALPHABETICAL AND NOT DECLARATION ORDER, and getting it wrong does
// not fail loudly: Discord accepts a channel naming a parent that does not exist
// yet by ignoring the parent, so the category ends up empty and everything sits at
// the top level looking like a styling problem.
func TestRolesComeBeforeChannelsAndCategoriesBeforeTheirChildren(t *testing.T) {
	ops, errs := Plan(CourtLayout())
	if len(errs) > 0 {
		t.Fatalf("the shipped layout is invalid: %v", errs)
	}
	lastRole, firstChannel := -1, -1
	for i, op := range ops {
		if op.Kind == "role" {
			lastRole = i
		}
		if op.Kind == "channel" && firstChannel < 0 {
			firstChannel = i
		}
	}
	if lastRole < 0 || firstChannel < 0 {
		t.Fatalf("the plan has %d roles and channels starting at %d; it is not "+
			"measuring the shipped layout", lastRole+1, firstChannel)
	}
	if lastRole > firstChannel {
		t.Error("a role is created after a channel; channel overwrites name roles by id")
	}

	pos := map[string]int{}
	for i, op := range ops {
		if op.Kind == "channel" {
			pos[op.Name] = i
		}
	}
	for _, ch := range CourtLayout().Channels {
		if ch.Parent == "" {
			continue
		}
		if pos[ch.Parent] > pos[ch.Name] {
			t.Errorf("channel %q is created before its parent %q", ch.Name, ch.Parent)
		}
	}
}

// The shipped layout must be consistent, and the validator must be able to say so
// about a broken one — a validator that never fires is the guard this repo keeps
// writing checks about.
func TestPlanRefusesTheMistakesAnEditorActuallyMakes(t *testing.T) {
	if _, errs := Plan(CourtLayout()); len(errs) != 0 {
		t.Fatalf("the shipped layout does not validate: %v", errs)
	}

	bad := []struct {
		name string
		l    Layout
	}{
		{"an overwrite for a role that is never created", Layout{
			Channels: []Channel{{Name: "c", Type: ChannelText,
				Overwrites: []Overwrite{{Role: "ghost", Allow: PermSendMessages}}}},
		}},
		{"a parent that is not a category", Layout{
			Channels: []Channel{
				{Name: "notacat", Type: ChannelText},
				{Name: "c", Type: ChannelText, Parent: "notacat"},
			},
		}},
		{"forum tags on a text channel", Layout{
			Channels: []Channel{{Name: "c", Type: ChannelText, Tags: []string{"x"}}},
		}},
		{"allowing and denying the same bit", Layout{
			Channels: []Channel{{Name: "c", Type: ChannelText,
				Overwrites: []Overwrite{{Role: EveryoneRole,
					Allow: PermSendMessages, Deny: PermSendMessages}}}},
		}},
		{"two channels with one name", Layout{
			Channels: []Channel{{Name: "c", Type: ChannelText}, {Name: "c", Type: ChannelText}},
		}},
		{"a role with no justification", Layout{
			Roles: []Role{{Name: "r"}},
		}},
	}
	for _, c := range bad {
		if _, errs := Plan(c.l); len(errs) == 0 {
			t.Errorf("%s: validated clean", c.name)
		}
	}
}

// More tags than Discord accepts fails the whole channel creation rather than
// truncating, so it is worth catching before the request.
func TestTooManyForumTagsIsRefused(t *testing.T) {
	tags := make([]string, maxForumTags+1)
	for i := range tags {
		tags[i] = string(rune('a' + i%26))
	}
	l := Layout{Channels: []Channel{{Name: "f", Type: ChannelForum, Tags: tags}}}
	if _, errs := Plan(l); len(errs) == 0 {
		t.Errorf("%d tags validated clean; Discord allows %d", len(tags), maxForumTags)
	}
}

// THE ONE OVERWRITE THE LAYOUT CANNOT LOSE.
//
// Without a default-deny on the court category, the docket is a channel anybody
// can post into and a mirrored verdict shares a scroll with whatever a stranger
// typed after it. That is the confusion the server exists to prevent, so it gets
// its own test rather than living inside the general validity check.
func TestTheCourtCategoryIsReadOnlyToEveryone(t *testing.T) {
	var found bool
	for _, ch := range CourtLayout().Channels {
		if ch.Type != ChannelCategory {
			continue
		}
		for _, o := range ch.Overwrites {
			if o.Role != EveryoneRole {
				continue
			}
			if o.Deny&PermSendMessages == 0 {
				t.Errorf("category %q does not deny SEND_MESSAGES to @everyone", ch.Name)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("no category carries an @everyone overwrite at all; either the layout " +
			"lost its default-deny or this test no longer knows where to look")
	}
}

// The forum is the exception to that deny, and it has to be stated rather than
// inherited — a court where nobody can argue is not the intent.
func TestReadersCanStillPostInsideAClaimThread(t *testing.T) {
	for _, ch := range CourtLayout().Channels {
		if ch.Type != ChannelForum {
			continue
		}
		for _, o := range ch.Overwrites {
			if o.Role == EveryoneRole && o.Allow&PermSendMessagesInThread != 0 {
				return
			}
		}
		t.Errorf("forum %q never re-allows SEND_MESSAGES_IN_THREADS, so the category's "+
			"deny leaves it read-only", ch.Name)
		return
	}
	t.Fatal("the layout has no forum; claim threads were the reason for one")
}
