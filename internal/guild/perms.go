// Package guild builds the two links that let a court's moderators stand up a
// Discord server for their court, and describes the layout that server gets.
//
// It is a LEAF PACKAGE ON PURPOSE. It imports nothing of ours, talks to no
// network, opens no database and holds no token: everything here is a pure
// function of its arguments, so the parts that are easy to get catastrophically
// wrong — the permission set the bot asks a stranger to grant, and the state
// value that ties an OAuth round-trip to one court — are testable without a
// Discord application, a chain, or a running service.
//
// The service that consumes this (the /api/guild handlers) and the gateway
// process that clerks the bound guilds are separate, later, and both depend on
// this package rather than the other way round.
package guild

import (
	"fmt"
	"sort"
	"strings"
)

// Permission is a Discord permission bitfield.
//
// Discord sends and receives these as a DECIMAL STRING, not a number, because
// the field long ago outgrew the 53-bit integer a JavaScript client can hold
// exactly. uint64 is the right Go type and String() is the right wire form; the
// bit positions below run to 40, so anything narrower silently truncates.
type Permission uint64

// The individual bits, named as Discord names them.
//
// Every one of these is spelled as a shift rather than a literal so the bit
// POSITION is what the source states. The positions are the durable fact —
// Discord's own reference is a table of positions — and a hand-computed literal
// is a transcription that nothing checks. 1<<40 is also the case that makes the
// point: written out it is 1099511627776, which is not a number anybody reviews.
const (
	PermCreateInstantInvite  Permission = 1 << 0
	PermKickMembers          Permission = 1 << 1
	PermBanMembers           Permission = 1 << 2
	PermManageChannels       Permission = 1 << 4
	PermManageGuild          Permission = 1 << 5
	PermAddReactions         Permission = 1 << 6
	PermViewChannel          Permission = 1 << 10
	PermSendMessages         Permission = 1 << 11
	PermManageMessages       Permission = 1 << 13
	PermEmbedLinks           Permission = 1 << 14
	PermAttachFiles          Permission = 1 << 15
	PermReadMessageHistory   Permission = 1 << 16
	PermManageRoles          Permission = 1 << 28
	PermManageWebhooks       Permission = 1 << 29
	PermManageThreads        Permission = 1 << 34
	PermCreatePublicThreads  Permission = 1 << 35
	PermSendMessagesInThread Permission = 1 << 38
	PermModerateMembers      Permission = 1 << 40
)

// NamedPermission pairs a bit with the reason the bot asks for it.
//
// THE REASON IS NOT DECORATION. This set is shown to a stranger on a consent
// screen and then held inside a server we do not own, so every entry has to be
// answerable to "why does a court clerk need to ban people". A bit nobody can
// justify in one line is a bit to drop, and the only way to notice one is to
// make the justification a field the compiler requires.
type NamedPermission struct {
	Bit  Permission
	Name string
	Why  string
}

// Required is exactly what the authorize URL asks for.
//
// NOT ADMINISTRATOR, and that is a deliberate trade. Administrator (1<<3) would
// be one bit instead of eighteen and would never be short of a power the clerk
// turns out to need. It is refused because this design's whole safety story is
// "kourt.xyz publishes the link only while the bot still holds what it was
// granted" — and Administrator collapses that check into a single boolean that
// tells you nothing about which powers were quietly removed. An enumerated set
// can be diffed against reality; a wildcard cannot.
//
// It is also the honest thing to put in front of somebody's consent screen.
var Required = []NamedPermission{
	{PermViewChannel, "VIEW_CHANNEL", "read the channels it clerks"},
	{PermSendMessages, "SEND_MESSAGES", "post the docket and verdicts"},
	{PermSendMessagesInThread, "SEND_MESSAGES_IN_THREADS", "post into a court's forum threads"},
	{PermCreatePublicThreads, "CREATE_PUBLIC_THREADS", "open a thread per claim"},
	{PermManageThreads, "MANAGE_THREADS", "retag and archive a claim thread as its phase changes"},
	{PermManageMessages, "MANAGE_MESSAGES", "remove messages impersonating the clerk"},
	{PermEmbedLinks, "EMBED_LINKS", "render a claim as an embed rather than a wall of text"},
	{PermAttachFiles, "ATTACH_FILES", "attach an exhibit"},
	{PermAddReactions, "ADD_REACTIONS", "mark a thread's state without a second message"},
	{PermReadMessageHistory, "READ_MESSAGE_HISTORY", "post in a thread it did not open"},
	{PermManageChannels, "MANAGE_CHANNELS", "provision the court layout"},
	{PermManageRoles, "MANAGE_ROLES", "issue the verified-party role"},
	{PermManageWebhooks, "MANAGE_WEBHOOKS", "mirror at volume without spending the message budget"},
	{PermCreateInstantInvite, "CREATE_INSTANT_INVITE", "mint the invite kourt.xyz publishes"},
	{PermKickMembers, "KICK_MEMBERS", "act on the clerk's own moderation"},
	{PermBanMembers, "BAN_MEMBERS", "act on the clerk's own moderation"},
	{PermModerateMembers, "MODERATE_MEMBERS", "time out rather than ban, where that is enough"},
	{PermManageGuild, "MANAGE_GUILD", "read the guild's invites and keep its name honest"},
}

// RequiredBits is the integer the authorize URL carries.
//
// Derived, never written down. A literal here would be a second definition of
// the set, and the failure it invites is the quiet one: two bits swap, the total
// is unchanged, and every check that compares totals stays green. The test pins
// the number AND the list for that reason.
func RequiredBits() Permission {
	var p Permission
	for _, n := range Required {
		p |= n.Bit
	}
	return p
}

// String renders a bitfield the way Discord's wire format wants it.
func (p Permission) String() string { return fmt.Sprintf("%d", uint64(p)) }

// Missing reports which required bits are absent from have, by name, sorted.
//
// This is the function the heartbeat is built on, and it returns NAMES rather
// than a bitfield because its output ends up in an operator's log and in the
// message telling a server owner what to restore. "MANAGE_THREADS" is actionable;
// "17179869184" is a number somebody has to go and decode.
//
// ELEVATED PERMISSIONS ARE WHY THIS CANNOT BE A ONE-TIME CHECK. Discord flags
// every Manage_* and moderation bit as elevated, and a guild with server-wide 2FA
// can refuse or strip them independently of what was granted at invite time. The
// grant is a starting position, not a guarantee.
func Missing(have Permission) []string {
	var out []string
	for _, n := range Required {
		if have&n.Bit == 0 {
			out = append(out, n.Name)
		}
	}
	sort.Strings(out)
	return out
}

// Explain renders the set as the lines a consent-screen explainer shows.
func Explain() string {
	var b strings.Builder
	for _, n := range Required {
		fmt.Fprintf(&b, "%-26s %s\n", n.Name, n.Why)
	}
	return b.String()
}
