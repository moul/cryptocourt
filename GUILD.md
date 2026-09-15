# GUILD.md — the Kourt ↔ Discord bridge

A court's moderators can stand up a Discord server for their court and have the
Kourt bot clerk it. This document is the operator's side: what exists today, what
does not, and the exact commands.

**Status: built, not yet switched on.** The service, the publish gate, the court
page block and the heartbeat all exist and are covered by tests. Nothing is live
because `--guild-client-id` is empty in the checked-in unit and there is no
Discord application behind it — see "Before any of this works" below.

One rough edge remains and it is named rather than hidden: publishing is a command
at a terminal, not a third click. See "What a moderator actually does today".

---

## Before any of this works

Everything below assumes a Discord application exists. Creating one is not
mentioned anywhere else in this document, and an operator who follows the command
sections verbatim without doing this first gets errors none of them explain.

At <https://discord.com/developers/applications> — **New Application**, then:

1. **OAuth2 → Redirects**: add `https://kourt.xyz/api/guild/bound`, exactly, byte
   for byte. Discord validates `redirect_uri` against this list and refuses an
   unregistered one. `--guild-redirect` must be the same string.
2. **OAuth2 → Require OAuth2 Code Grant**: **on**. Off, and Discord ignores
   `response_type=code` for a bot invite and silently runs the plain invite flow:
   the bot joins, the browser never comes back, no code is ever issued, and the
   callback never fires. Nothing errors.
   (`discord/discord-api-docs#2069` asked for the parameters to work without this
   and was closed wontfix.)
3. **Bot → Public Bot**: **on**, or nobody but the application's owner can add it.
4. **Bot → Privileged Gateway Intents**: leave them all **off**. None is needed,
   and Message Content in particular is deliberately declined — see the note in
   `internal/guild/urls.go`.
5. Copy three values: the **Application ID** (→ `--guild-client-id`), the **OAuth2
   client secret** (→ the secret file), and the **bot token** (→ the token file).

You also need Discord's **Developer Mode** on — *User Settings → Advanced* — to
copy a server's numeric id, which `build-template --guild` and `claim --guild`
both take. Right-click a server, **Copy Server ID**.

Finally, `build-template` needs a **reference guild**: an ordinary Discord server
you create by hand and add the bot to, whose layout becomes the template every
court server is copied from. It is never published and no reader ever sees it.

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

    kourtguildctl mod --court meta --addr g1…

**Run this first.** Asks the live chain whether that address may publish the
court's server, and reports the court's m-of-n either way. Everything below is
preparation for something only a moderator can publish, so an address that is not
one is a wasted afternoon. `--rpc` and `--pkg` point it at a different node or
realm; the defaults are kourt-1.

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

## The service

Four routes, mounted on the chat service's own mux the way `internal/archive` is.
**Only one of them grants anything.**

| Route | What it does |
|---|---|
| `POST /api/guild/start?chain=&court=` | mints a single-use nonce, returns the authorize link |
| `GET /api/guild/bound` | Discord's redirect target; records the guild as `pending` |
| `POST /api/guild/claim` | the only route that publishes a server |
| `GET /api/guild/servers/{chain}/{court}` | what the overlay reads |

`pending` grants nothing — no provisioning, no docket, no link. Anyone can build
the authorize URL out of the client id in the page source and install the bot into
a guild they control, so `bound` is reachable by strangers and deliberately leaves
nothing behind that matters.

`/bound` takes the guild id from the **token-exchange response**, never from the
redirect's `guild_id` parameter — Discord's own documentation calls that a hint.

### The three checks on a claim

In this order, and each is necessary:

1. **the nonce** was minted here and has not been spent
2. **the signature** covers a challenge naming this court, this guild and this
   nonce, and the key derives to the address claimed
3. **the chain** says that address moderates that court

Dropping 1 makes a signature a reusable warrant. Dropping 2 lets anyone claim any
address. Dropping 3 is the front-running this whole design exists to prevent.

The challenge is built from the **stored flow**, not from the request body, so a
claim naming one court in its JSON and signing for another is refused rather than
resolved in favour of whichever the handler read second. A failed claim still
spends its nonce — otherwise a wrong signature is free to retry.

A chain that did not answer is **not** a chain that said no: `/claim` returns 503
and publishes nothing. The reverse rule applies to the heartbeat, which must not
delist on an unreachable node, or one blip takes down every court's listing.

## Running it

The bridge rides the **existing chat service** — `internal/archive`'s shape, on
`kourtchat`'s own mux and listener. There is no second binary and no second port,
so a deploy ships nothing new: `make chat` already builds it in.

Three things have to be in place, and until they are the bridge is OFF and says so
in the log at startup. No court page shows a server while it is off.

1. **The unit's four flags** (`deploy/kourtchat.service`). `--guild-client-id` is
   empty in the checked-in unit; fill it in with the Discord application's id.
   `--guild-client-secret-file` and `--guild-token-file` are PATHS, never the
   secrets themselves — a bot token on an `ExecStart` line is in the process table
   for anybody with a shell.
2. **The two secret files**, mode 0600 under `/var/lib/kourt/secret/`, beside the
   IP-hashing key and never copied by `deploy.sh`:

       /var/lib/kourt/secret/discord_token
       /var/lib/kourt/secret/discord_client_secret

3. **The nginx location**, `location /api/guild/` in `deploy/nginx.conf` —
   installed by **`make setup`, not by a deploy**. Miss it and the SPA fallback in
   `location /` answers every `/api/guild/` request with index.html and a 200,
   which the overlay hands to `JSON.parse`.

## Keeping a listing honest

`--guild-heartbeat` (default 30 minutes) re-checks every published server: that
the bot is still in it, still holds all eighteen permissions, and that the address
which published it still moderates the court. Any of those failing **hides the
link** — the binding goes `degraded`, which is not `listed`, so the court page
stops showing it at once. It comes back by itself when the cause clears.

A degraded listing is kept for a week before it is delisted. The failures being
waited through are a permission an owner will restore when told and a mod-set
change governance is mid-way through; both are measured in days, and the link is
already hidden throughout.

**The fail direction is the opposite of `/claim`'s.** Publishing on an answer
nobody got would be wrong, so `/claim` refuses when the chain is unreachable.
*Delisting* on an answer nobody got would be worse — one node blip would take
every court's listing down at once — so the heartbeat changes nothing it could not
confirm. Setting `--guild-heartbeat 0` disables it; kourtchat says plainly at
startup that listings will then never be re-checked.

The cadence is jittered by up to a quarter either way. A fixed period is a window
an owner can work inside: strip the permissions just after a sweep, restore them
before the next, and every check the site makes says healthy.

Guilds the bot was added to but nobody ever published are **left after 24 hours**.
Anyone can install the bot — the client id is in the page source by necessity — so
presence proves nothing, and what it still costs is the bot sitting in a stranger's
server able to enumerate it. Guilds that were ever listed are never left, even
delisted ones: they have provisioned channels and a history.

`--archive-rpc` is required when the bridge is on, and kourtchat refuses to start
without it rather than falling back: the publish gate IS a chain read, and a bridge
that cannot ask whether a signer moderates a court must not run.

## Checking a deploy

Four commands, no Discord application needed, all against the running service.
They exercise the wiring that unit tests cannot: the flags, the mount, the store
and the chain read, in one process.

    # 1. the bridge is on, and says which redirect it will use
    journalctl -u kourtchat -n 50 | grep -E "Discord bridge|re-checking"

    # 2. a court with no server answers cleanly, rather than 404ing
    curl -s https://kourt.xyz/api/guild/servers/kourt-1/meta
    # {"server":null}

    # 3. a flow can be started, and the link it returns is one Discord honours
    curl -s -X POST "https://kourt.xyz/api/guild/start?chain=kourt-1&court=meta"
    # {"authorize":"https://discord.com/oauth2/authorize?...response_type=code...","nonce":"…"}

    # 4. a bad slug is refused rather than reaching the chain
    curl -s -X POST "https://kourt.xyz/api/guild/start?chain=kourt-1&court=MY-COURT"
    # {"error":"that is not a court slug"}

**If step 2 returns HTML with a 200**, nginx has no `/api/guild/` location and the
SPA fallback is answering — re-run `make setup`. That is the single likeliest
deployment mistake and it looks like a JSON parser bug from the overlay.

Three refusals worth knowing, each verified to fail at startup rather than later:
`--guild-client-id` with no `--archive-rpc`; an empty or missing secret file; and
`--guild-heartbeat 0`, which starts but says loudly that listings will never be
re-checked.

    kourtguildctl claim --court meta --guild <server id> --nonce <nonce> --key <name>

Signs for a server and publishes it. This is the step that makes a listing real,
and the only one that needs a moderator's key — "Publishing a server, start to
finish" below walks the whole sequence.

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

## Publishing a server, start to finish

    # 1. may this key publish it at all?
    kourtguildctl mod --court meta --addr g1…

    # 2. the two links, and the nonce the claim will need
    kourtguildctl links --court meta --client-id <app id> \
      --redirect https://kourt.xyz/api/guild/bound --template <code>

    # ...click link 1 to create the server, link 2 to add the bot...

    # 3. sign for it and publish
    kourtguildctl claim --court meta --guild <server id> \
      --nonce <from step 2> --key deployer

Step 3 prints the challenge before asking for the passphrase, which is what a
wallet prompt would do — somebody about to publish an outbound link under a
court's name should read what they are agreeing to. The passphrase is read from
the terminal, never a flag: a key that can publish a court's server deserves the
same care this tool gives the bot token.

`--home` points at a keybase other than gnokey's, `--service` at a Kourt other
than kourt.xyz.

## What a moderator actually does today

Adding the bot is two clicks. **Publishing is a third step at a terminal**, not a
click, and that is the one rough edge left in the flow.

The claim is a signature over a challenge — court, guild id and nonce — and the
overlay cannot produce it. Adena is wired into the page for *transactions*
(`AddEstablish`, `DoContract`, `GetAccount`); an arbitrary-message signature is a
different call and the overlay does not make it. So the browser can start the
flow and the service can verify a claim, and nothing joins the two.

Until it does, the court page says so rather than offering a button that
dead-ends: an unlisted court reads *"its moderators can publish one — adding the
bot is done from here, and publishing is signed with the moderator's own key"*,
and a moderator returning from Discord is told the bot is in but not published and
what is left.

`kourtguildctl claim` closes it from the side that needs no wallet support, and
is what the section above documents. Making it a click as well needs
arbitrary-message signing in the overlay, which is a wallet-integration question
rather than a bridge one.

## Still open

Two are the owner's call, not the bridge's:

- **Whether publishing should need m-of-n.** Every other set-level moderator act
  in the realm goes through `approveAction`; this one takes a single signature. On
  a court with 3-of-5 moderators, any one of the five can publish or displace a
  server against the other four. `ModThreshold` is already read and available;
  wiring it in is small. `internal/binding/chain.go` records the argument.
- **Whether a court's moderators should control an outbound link at all.**
  `realm/r/kourtv2/sitelink.gno` deliberately refuses them one, reserving the
  platform's domain to the global DAO admin: *"a court that could point its own
  pages at a domain of its choosing could route its readers anywhere while wearing
  the platform's name."* A Discord listing is substantially that power, labelled.

And one is operational: **the 100-guild verification wall.** An unverified bot is
hard-blocked at 100 guilds. Apply before launch, not when it is hit, and mention
the 24-hour auto-leave in the application — join-and-leave churn is a pattern
reviewers watch for, and the rule producing it here is defensible but easier to
explain in advance than afterwards.

## Not built

Nothing provisions channels inside a user's server, and nothing needs to: the
server is created from the template, which already carries the layout. The bot
posts the notice and mints the invite; it does not build the room.

Nothing mirrors the docket into a listed server. That was the original phase-2
idea and it is still a reasonable feature, but it is additive — a court can have a
published, healthy, honestly-labelled Discord without it.
