# GUILD.md — the Kourt ↔ Discord bridge

A court's moderators can stand up a Discord server for their court and have the
Kourt bot clerk it. This document is the operator's side: what exists today, what
does not, and the exact commands.

**Status: phase 1.** The layout, the links and the template tool exist and are
covered by tests. There is no service and no bot process yet, so nothing on
kourt.xyz shows a Discord link and no guild is clerked. What phase 1 gives you is
the ability to mint the template and hand somebody the two links by hand.

---

## Why two clicks and not one

A Discord **template cannot carry a bot**. It snapshots structure — channels,
roles, overwrites, settings — and a bot is a member, so a server created from a
template arrives with none in it. So the flow is:

1. **create the server** from the template — `https://discord.new/<code>`
2. **add the bot** — an OAuth2 authorize link carrying the court in its `state`

Discord's `POST /guilds` cannot be used to skip step 1: it is restricted to bots
in fewer than 10 guilds. Guild ownership also cannot be transferred to a bot —
that call silently does nothing on a bot account.

## Two portal settings, or step 2 does nothing

Both live in the Discord developer portal and neither can be set from a link.
Getting either wrong makes everything **look** right:

- **Public Bot** — off, and nobody but the application's owner can add the bot.
- **Require OAuth2 Code Grant** — off, and Discord ignores `response_type` and
  `redirect_uri` for a bot-scoped invite. It runs the plain invite flow instead:
  the bot joins, the browser never comes back, no code is issued, and the callback
  never fires. Nothing errors anywhere.
  (`discord/discord-api-docs#2069` asked for the parameters to work without the
  toggle and was closed wontfix, so this is permanent.)

## Who may publish a court's server

The court's **on-chain moderator set**, and nobody else. A binding is published
only if signed by an address for which the chain answers
`IsCourtMod(courtSlug, addr) == true` — `realm/r/kourtv2/moderation.gno:1339` —
re-checked on every heartbeat.

This is what makes front-running impossible: whoever races to create a Discord and
bind it first gets nothing, because being first is not the qualification. It does
**not** make a court legitimate — court creation is permissionless, and a court's
creator is trivially its own moderator, so anyone can make their own court and
publish a server for it. The listing means what it says and no more:

> Listed means this court's current moderators chose this server. It does not mean
> the court is legitimate, or that anything said here is true.

On kourt-1 the moderator of `meta` and `covid` is the `deployer` key
(`g174hxvmvs7fsg50gy7xu8x9eyqa4chdv8tclngz`), not any personal wallet. Check
before assuming:

    D=$(printf '%s' 'gno.land/r/kourt/kourtv2.IsCourtMod("meta","g1…")' | base64)
    curl -s --get https://rpc.kourt.xyz/abci_query \
      --data-urlencode 'path="vm/qeval"' --data-urlencode "data=$D"

## The permission set

The bot asks for eighteen enumerated permissions, not Administrator. Administrator
is refused deliberately: the delisting design rests on diffing granted powers
against expected ones, and a wildcard makes that diff meaningless. See them with
their justifications:

    kourtguildctl perms

Every one is an **elevated** permission in Discord's terms, which means a guild
with server-wide 2FA will refuse them unless the operator's own Discord account
has 2FA enabled.

## Commands

    kourtguildctl plan

Prints the court layout as the ordered calls that would build it. Touches nothing.

    kourtguildctl perms

Prints the permission set, each bit's justification, and the total.

    kourtguildctl links --court meta --chain kourt-1 \
      --client-id <app id> --redirect https://kourt.xyz/api/guild/bound \
      --template <code>

Prints the two links for one court. `--template` is optional; without it, step 1
says the template has not been minted yet.

The nonce in the printed link is generated locally. In the real flow the service
mints it, stores it against a short-lived session and redeems it once — that is
what makes it evidence the redirect belongs to a browser that started the flow. A
link printed at a terminal has no session behind it.

    kourtguildctl build-template --guild <reference guild id>
    kourtguildctl build-template --guild <reference guild id> --apply

Applies the layout to a reference guild and mints (or syncs) its template.
**Dry run is the default** — without `--apply` it prints the plan and sends
nothing. `--dry-run` is accepted so a script can say the safe thing out loud, and
`--name` sets the template's name.

A guild holds exactly one template (Discord error 30031 if it already has one), so
this creates on the first run and syncs afterwards. That means **one reference
guild per template**: a second layout variant needs a second reference guild.

## The bot token

Read from `$KOURT_DISCORD_TOKEN`. Never a flag — a flag puts a credential that can
ban people into every process listing on the box, which is the same reason the
faucet's mnemonic goes through a systemd credential rather than an `ExecStart`
argument.

It is a bearer credential whose blast radius is every guild at once, and rotating
it is a developer-portal action, not a file edit. Treat it as a different class of
secret from the chat service's IP-hashing key.

## Limits worth knowing before you design around them

| Limit | Value |
|---|---|
| Channels per guild | 500 (threads are exempt) |
| Children per category | 50 |
| Forum tags | 20 available, 5 applied per thread |
| Guilds an **unverified** bot may join | **100** (verification opt-in from 75) |
| Global request budget | 50/second, shared across every guild |
| Channel name/topic edits | 2 per 10 minutes, per channel |

The 100-guild wall is the one that shapes the roadmap: past it, the bot cannot
join anything until it passes Discord's verification — ToS and privacy-policy
URLs, identity verification, roughly a week, and discretionary. Apply before
launch, not when the wall is hit.

## Not built yet

Phase 1b is the service (`/api/guild/*`, the OAuth callback, `IsCourtMod`
verification) and the court-page block. Phase 2 is the gateway process that
provisions a bound guild, mirrors the docket, and runs the heartbeat that delists
a server which has stripped the bot's permissions.

Three things must be settled before phase 1b is written: the exact wording of the
"listed means" copy above, the signed-challenge payload (already implemented as
`guild.ChallengeText`, binding court, guild id and nonce), and whether publishing
requires the same m-of-n moderator approval that every other set-level act in the
realm uses.
