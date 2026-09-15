// Package discordapi is the smallest Discord REST client that does Kourt's jobs.
//
// IT IS A PACKAGE RATHER THAN PART OF THE CLI BECAUSE GO FORBIDS THE ALTERNATIVE.
// This started inside cmd/kourtguildctl, where it was reachable by exactly one
// caller — and the two things GUILD.md says come next, the /api/guild service and
// the gateway process, cannot import a main package at all. Left where it was, it
// would have been copied twice rather than reused, and a token-handling client
// existing in three edited copies is the arrangement where one of them keeps a
// bug the others fixed.
//
// It stays separate from internal/guild, which touches no network and holds no
// credential. That separation is what lets the rules in that package — the
// permission set, the state grammar — be unit-tested without a Discord
// application, and it is worth keeping.
package discordapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jaekwon/kourt/internal/guild"
)

// ErrTemplateExists is Discord's "this guild already has a template".
//
// It is a named error rather than a string match at the call site because the
// response is an INSTRUCTION, not a failure: a guild holds exactly one template,
// so 30031 means "sync the one that is there" and treating it as an error means
// refusing to update the template every court server is copied from.
const codeTemplateExists = 30031

var ErrTemplateExists = fmt.Errorf("discordapi: guild already has a template (%d)", codeTemplateExists)

// maxReplyBytes caps what is read from Discord.
//
// /claim bounds its inbound body and nothing bounded the outbound replies, which
// is the asymmetry worth closing rather than any specific exploit: Discord is
// trusted by configuration, but "trusted" is a property of the config being right,
// and a misdirected base URL should cost a bounded read rather than the process.
// Four megabytes is far above the largest real reply — a 200-guild page.
const maxReplyBytes = 4 << 20

// DefaultBaseURL is Discord's v10 REST root.
const DefaultBaseURL = "https://discord.com/api/v10"

// Client talks to Discord as a bot.
//
// BaseURL is a field rather than a constant for one reason: without it nothing
// here can be tested. Every method sends a token and mutates somebody's guild, so
// the alternative to a test server is testing against a real Discord — which
// means the ordering, the error mapping and the create-or-sync branch would each
// be verified for the first time on the day they were used in anger.
type Client struct {
	Token   string
	BaseURL string
	HTTP    *http.Client

	// me is the bot's own user id, learned once. See Me().
	me string
}

// New returns a client with a timeout that is bounded but not stingy: guild
// creation calls are slower than reads and a 10s ceiling turns a normal slow day
// into a half-applied layout.
func New(token string) *Client {
	return &Client{
		Token:   token,
		BaseURL: DefaultBaseURL,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Do performs one call. out may be nil.
func (c *Client) Do(method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequest(method, base+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	// Discord asks for a User-Agent naming the project and a contact URL, and
	// enforces it on some routes.
	req.Header.Set("User-Agent", "Kourt (https://kourt.xyz, 0.1)")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxReplyBytes))

	if resp.StatusCode/100 != 2 {
		// The body is echoed because Discord's error CODES are specific where the
		// HTTP status is not — a bare 400 hides which of a dozen things went
		// wrong, and the code is the part an operator can look up.
		var e struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(b, &e)
		if e.Code == codeTemplateExists {
			return ErrTemplateExists
		}
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(b)))
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}

// Progress is called once per created object so a caller can report as it goes.
// A layout takes several seconds to apply and silence for that long reads as a hang.
type Progress func(kind, name, id string)

// ApplyLayout creates the layout's roles and channels in a guild.
//
// It does NOT re-derive the creation order: guild.Ordered is the single authority
// on "categories before their children", and this used to compute the same thing
// separately, in a different shape, from the same input. Discord accepts a channel
// naming a parent that does not exist yet by ignoring the parent, so the two
// copies disagreeing would not have failed — it would have produced an empty
// category and a flat server.
func (c *Client) ApplyLayout(guildID string, l guild.Layout, onCreate Progress) error {
	if onCreate == nil {
		onCreate = func(string, string, string) {}
	}
	// @everyone's role id IS the guild id, which is why overwrites can name it
	// before anything has been created.
	roleID := map[string]string{guild.EveryoneRole: guildID}
	for _, r := range l.Roles {
		var got struct {
			ID string `json:"id"`
		}
		body := map[string]any{
			"name":        r.Name,
			"permissions": r.Permissions.String(),
			"hoist":       r.Hoist,
			"mentionable": r.Mentionable,
		}
		if err := c.Do("POST", "/guilds/"+guildID+"/roles", body, &got); err != nil {
			return fmt.Errorf("creating role %q: %w", r.Name, err)
		}
		roleID[r.Name] = got.ID
		onCreate("role", r.Name, got.ID)
	}

	chanID := map[string]string{}
	for _, ch := range guild.Ordered(l.Channels) {
		body := map[string]any{"name": ch.Name, "type": int(ch.Type)}
		if ch.Topic != "" {
			body["topic"] = ch.Topic
		}
		if ch.Parent != "" {
			id, ok := chanID[ch.Parent]
			if !ok {
				return fmt.Errorf("channel %q wants parent %q, which was not created", ch.Name, ch.Parent)
			}
			body["parent_id"] = id
		}
		if len(ch.Tags) > 0 {
			tags := make([]map[string]any, 0, len(ch.Tags))
			for _, t := range ch.Tags {
				tags = append(tags, map[string]any{"name": t, "moderated": false})
			}
			body["available_tags"] = tags
		}
		if len(ch.Overwrites) > 0 {
			ows := make([]map[string]any, 0, len(ch.Overwrites))
			for _, o := range ch.Overwrites {
				id, ok := roleID[o.Role]
				if !ok {
					return fmt.Errorf("channel %q overwrites role %q, which was not created", ch.Name, o.Role)
				}
				ows = append(ows, map[string]any{
					"id": id, "type": 0, // 0 = role, 1 = member
					"allow": o.Allow.String(), "deny": o.Deny.String(),
				})
			}
			body["permission_overwrites"] = ows
		}
		var got struct {
			ID string `json:"id"`
		}
		if err := c.Do("POST", "/guilds/"+guildID+"/channels", body, &got); err != nil {
			return fmt.Errorf("creating channel %q: %w", ch.Name, err)
		}
		chanID[ch.Name] = got.ID
		onCreate(ch.Type.String(), ch.Name, got.ID)
	}
	return nil
}

// EnsureTemplate mints the guild's template, or syncs the one it already has, and
// returns the code. The second return says which happened, because "synced" is
// the answer an operator needs when they expected a fresh code.
func (c *Client) EnsureTemplate(guildID, name, description string) (code string, synced bool, err error) {
	var tpl struct {
		Code string `json:"code"`
	}
	body := map[string]any{"name": name, "description": description}
	err = c.Do("POST", "/guilds/"+guildID+"/templates", body, &tpl)
	if err == nil {
		return tpl.Code, false, nil
	}
	if err != ErrTemplateExists {
		return "", false, err
	}

	var existing []struct {
		Code string `json:"code"`
	}
	if err := c.Do("GET", "/guilds/"+guildID+"/templates", nil, &existing); err != nil {
		return "", false, err
	}
	if len(existing) == 0 {
		return "", false, fmt.Errorf("discordapi: guild %s reports it already has a template, but lists none", guildID)
	}
	// PUT is "sync to the guild's current state", which is the whole point of
	// re-running build-template after editing the layout.
	if err := c.Do("PUT", "/guilds/"+guildID+"/templates/"+existing[0].Code, nil, &tpl); err != nil {
		return "", false, fmt.Errorf("syncing template %s: %w", existing[0].Code, err)
	}
	return existing[0].Code, true, nil
}

// Token is what the code grant returns.
//
// THE GUILD OBJECT IS THE POINT, not the access token. Discord's own docs call
// the redirect's guild_id parameter a HINT and say to require the code grant if
// you need to be sure — this nested object is what "sure" means, because it comes
// back over a server-to-server call authenticated with the client secret rather
// than over a redirect the browser could have been steered into.
//
// The access token is returned too, and callers should generally discard it.
// Nothing in this bridge acts as the installing user; the bot has its own
// authority. A stored user token is a credential with no job.
type Token struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Guild       *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"guild"`
}

// ErrNoGuildInToken means Discord accepted the code but returned no guild, which
// is what a non-bot authorisation looks like — the wrong flow, not a failure.
var ErrNoGuildInToken = errors.New("discordapi: the token response carried no guild; " +
	"this was not a bot install, or Require OAuth2 Code Grant is off")

// ExchangeCode redeems an authorization code.
//
// It does NOT go through Do(): the token endpoint takes form encoding rather than
// JSON and authenticates with the client secret, so it is the one call in this
// package that is not a bot-token request. Keeping it separate is also why the
// secret appears in exactly one function.
func (c *Client) ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)

	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	// Basic auth rather than client_secret in the body: both are accepted, and
	// this keeps the secret out of anything that logs a request body.
	req.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(clientSecret))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Kourt (https://kourt.xyz, 0.1)")

	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxReplyBytes))
	if resp.StatusCode/100 != 2 {
		// The body is echoed, but the secret never is — it went in a header.
		return nil, fmt.Errorf("token exchange: %s: %s", resp.Status, strings.TrimSpace(string(b)))
	}
	var t Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("token exchange: unreadable reply: %w", err)
	}
	if t.Guild == nil || t.Guild.ID == "" {
		return nil, ErrNoGuildInToken
	}
	return &t, nil
}

// ErrNoInvitableChannel means the bot can see no channel it may invite to. That
// is a permissions answer, not a failure: a guild where CREATE_INSTANT_INVITE was
// withheld, or where every channel denies it to the bot's role.
var ErrNoInvitableChannel = errors.New("discordapi: no channel this bot may invite to")

// EnsureInvite returns a permanent invite to a guild, reusing one the bot already
// made rather than minting a second.
//
// MAX_AGE AND MAX_USES ARE BOTH ZERO, WHICH MEANS UNLIMITED, and neither is a
// default worth inheriting. Discord's default invite expires in 24 hours: a link
// published on a court page that quietly stops working the next day is worse than
// no link, because the page keeps claiming there is a server. A use cap is worse
// still — a rival could exhaust it with throwaway accounts and take the court's
// listing down without touching the court.
//
// REUSE BEFORE CREATE for the same reason: re-running this must not leave a guild
// carrying a dozen equivalent invites, and an invite already published somewhere
// should keep working.
func (c *Client) EnsureInvite(guildID string) (string, error) {
	// An existing unlimited invite made by this bot is the best answer.
	var existing []struct {
		Code      string `json:"code"`
		MaxAge    int    `json:"max_age"`
		MaxUses   int    `json:"max_uses"`
		Temporary bool   `json:"temporary"`
		Revoked   bool   `json:"revoked"`
	}
	if err := c.Do("GET", "/guilds/"+guildID+"/invites", nil, &existing); err == nil {
		for _, i := range existing {
			if i.MaxAge == 0 && i.MaxUses == 0 && !i.Temporary && !i.Revoked && i.Code != "" {
				return "https://discord.gg/" + i.Code, nil
			}
		}
	}
	// A guild whose invites cannot be listed is not a guild that cannot be
	// invited to — MANAGE_GUILD is what reads that list, and it may have been
	// withheld while CREATE_INSTANT_INVITE was granted. So a failure above falls
	// through rather than returning.

	best, err := c.firstTextChannel(guildID, "")
	if err != nil {
		return "", err
	}

	var made struct {
		Code string `json:"code"`
	}
	body := map[string]any{"max_age": 0, "max_uses": 0, "temporary": false, "unique": false}
	if err := c.Do("POST", "/channels/"+best+"/invites", body, &made); err != nil {
		return "", err
	}
	if made.Code == "" {
		return "", ErrNoInvitableChannel
	}
	return "https://discord.gg/" + made.Code, nil
}

// MyGuilds returns every guild this bot is in, with the permissions it holds
// there.
//
// ONE CALL FOR ALL OF THEM, WHICH IS THE WHOLE REASON THIS SHAPE WAS CHOSEN. The
// obvious heartbeat asks each guild about itself, which is one request per listed
// court per sweep against a budget of 50 requests/second shared by everything the
// bot does. /users/@me/guilds pages 200 at a time, so a hundred courts cost one
// call rather than a hundred — and a court whose bot was kicked shows up as an
// ABSENCE rather than a 403, which matters: Discord bans an IP for 24 hours after
// 10,000 cumulative 401/403/429 responses in ten minutes, and a per-guild sweep
// over revoked guilds is exactly the pattern that reaches that.
//
// The permissions here are GUILD-LEVEL and do not account for channel overwrites.
// That is the right altitude for "has the bot been demoted": an owner who strips a
// single channel has not withdrawn the grant, and an owner who withdraws the grant
// shows up here.
func (c *Client) MyGuilds(ctx context.Context) (map[string]guild.Permission, error) {
	out := map[string]guild.Permission{}
	after := ""
	for {
		path := "/users/@me/guilds?limit=200"
		if after != "" {
			path += "&after=" + after
		}
		var page []struct {
			ID          string `json:"id"`
			Permissions string `json:"permissions"`
		}
		if err := c.Do("GET", path, nil, &page); err != nil {
			return nil, err
		}
		for _, g := range page {
			var p uint64
			// A permissions field that will not parse is treated as zero, which
			// reads as "holds nothing" and degrades the listing. That is the safe
			// direction for a value this code cannot understand — the unsafe one
			// is assuming everything is fine.
			_, _ = fmt.Sscanf(g.Permissions, "%d", &p)
			out[g.ID] = guild.Permission(p)
			after = g.ID
		}
		if len(page) < 200 {
			return out, nil
		}
	}
}

// Leave removes the bot from a guild.
//
// UNRESTRICTED AND PERMISSION-FREE: a bot may always leave. This is what makes
// the 24-hour auto-leave possible at all — a guild nobody ever published is one
// the bot should not be sitting in, watching, for the rest of its life.
//
// Worth knowing rather than discovering: frequent join-and-leave churn across
// many guilds is a pattern Discord's abuse heuristics and its human verification
// reviewers watch for. The rule that produces it here is defensible — leave what
// was never claimed — and it belongs in the verification application rather than
// being explained after the fact.
func (c *Client) Leave(guildID string) error {
	return c.Do("DELETE", "/users/@me/guilds/"+guildID, nil, nil)
}

// Me returns the bot's own user id, cached for the life of the client.
//
// A READER CAN ONLY TELL THE CLERK APART BY ITS ID. An owner controls every
// nickname, avatar and webhook in their own guild, so "the message says Kourt
// Clerk" proves nothing — internal/chat/bot.go had to learn the same thing about
// monikers and build a reserved-name rule. Discord gives no equivalent, so the
// id is what the disclosure cites and what a reader can check.
func (c *Client) Me() (string, error) {
	if c.me != "" {
		return c.me, nil
	}
	var u struct {
		ID string `json:"id"`
	}
	if err := c.Do("GET", "/users/@me", nil, &u); err != nil {
		return "", err
	}
	if u.ID == "" {
		return "", errors.New("discordapi: /users/@me returned no id")
	}
	c.me = u.ID
	return u.ID, nil
}

// disclosureMark is how the bot recognises its OWN notice on a later run. It is
// visible text rather than a hidden character: a reader who wonders why a pinned
// message is there should be able to read the answer, and a marker somebody can
// see is a marker somebody can quote back.
const disclosureMark = "/#/c/"

// EnsureDisclosure pins a notice in the guild saying what being listed means.
//
// THE SENTENCE BELONGS WHERE THE READER IS. kourt.xyz carries it beside the link,
// which serves the person who arrives through the court page and nobody who
// arrives through a shared invite — and an invite is how most people will arrive.
//
// IT IS LEGIBILITY, NOT A GUARANTEE, and the difference is worth being plain
// about: the guild's owner can unpin this, delete it, or post something next to it
// that contradicts it. Their guild, their powers. What it buys is that a reader
// who looks can find out, and that the clerk has said the true thing once in the
// room rather than only on a site the reader may never open.
//
// IDEMPOTENT BY REWRITING ITS OWN PIN rather than by remembering a message id: a
// stored id goes stale the moment somebody deletes the message, and a bot that
// then posts a second one every sweep is a bot that spams the room it is trying
// to be trustworthy in.
func (c *Client) EnsureDisclosure(guildID, chain, court, site string) error {
	me, err := c.Me()
	if err != nil {
		return err
	}
	ch, err := c.firstTextChannel(guildID, "how-this-works")
	if err != nil {
		return err
	}
	body := disclosureText(chain, court, site, me)

	var pins []struct {
		ID     string `json:"id"`
		Author struct {
			ID string `json:"id"`
		} `json:"author"`
		Content string `json:"content"`
	}
	if err := c.Do("GET", "/channels/"+ch+"/pins", nil, &pins); err == nil {
		for _, p := range pins {
			if p.Author.ID == me && strings.Contains(p.Content, disclosureMark) {
				if p.Content == body {
					return nil // already correct; do not touch the room
				}
				return c.Do("PATCH", "/channels/"+ch+"/messages/"+p.ID,
					map[string]any{"content": body}, nil)
			}
		}
	}
	// Unreadable pins are not a reason to skip the notice — MANAGE_MESSAGES reads
	// that list and may be withheld while sending is allowed.

	var made struct {
		ID string `json:"id"`
	}
	if err := c.Do("POST", "/channels/"+ch+"/messages",
		map[string]any{"content": body}, &made); err != nil {
		return err
	}
	// Pinning is best-effort: an unpinned notice is still a posted notice, and
	// failing the whole call over the pin would lose the message that succeeded.
	if err := c.Do("PUT", "/channels/"+ch+"/pins/"+made.ID, nil, nil); err != nil {
		return nil
	}
	return nil
}

// firstTextChannel finds somewhere to put a message or an invite.
//
// THE FIRST TEXT CHANNEL BY POSITION, which is where a reader arriving through an
// invite lands. Categories (type 4) and voice are skipped; a forum (15) is
// skipped too, because an invite that drops somebody into a list of threads reads
// as a broken link even though it worked.
//
// prefer names a channel to take over the positional pick when it exists — the
// layout's own how-this-works, for the notice. Empty means position decides.
//
// ONE WALK, NOT TWO. The invite and the notice each had their own copy of this
// loop, differing only in that one of them knew about a preferred name; two
// answers to "where does this guild's first text channel live" is two places for
// the next channel-type exclusion to be forgotten.
func (c *Client) firstTextChannel(guildID, prefer string) (string, error) {
	var channels []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type int    `json:"type"`
		Pos  int    `json:"position"`
	}
	if err := c.Do("GET", "/guilds/"+guildID+"/channels", nil, &channels); err != nil {
		return "", err
	}
	best, bestPos := "", 1<<30
	for _, ch := range channels {
		if ch.Type != 0 {
			continue
		}
		if prefer != "" && ch.Name == prefer {
			return ch.ID, nil
		}
		if ch.Pos < bestPos {
			best, bestPos = ch.ID, ch.Pos
		}
	}
	if best == "" {
		return "", ErrNoInvitableChannel
	}
	return best, nil
}

// disclosureText is the notice itself.
//
// IT SAYS THE SAME THING AS THE COURT PAGE, in the same order, because a reader
// who checks both and finds two different accounts of what a listing means has
// been given a reason to trust neither. scripts/check-guild-copy.py holds the
// site's two copies together; this is the third and it quotes the same clause.
func disclosureText(chain, court, site, botID string) string {
	if site == "" {
		site = "https://kourt.xyz"
	}
	return "**This server is listed for the " + court + " court on " + site + "**\n\n" +
		"Listed means this court's current moderators chose this server. " +
		"It does not mean the court is legitimate, or that anything said here is true.\n\n" +
		"This server is run by its own owner, not by the court and not by Kourt. " +
		"Nothing said here is part of the court's record — the record is on chain, " +
		"at " + strings.TrimSuffix(site, "/") + disclosureMark + court + ".\n\n" +
		"Messages from the clerk come from <@" + botID + "> and from no one else. " +
		"Anyone in a Discord server can choose a name and a picture, so that id is " +
		"the only thing that distinguishes the clerk from somebody imitating it.\n\n" +
		"_Chain: " + chain + ". The court's moderators can unpublish this server at any time._"
}
