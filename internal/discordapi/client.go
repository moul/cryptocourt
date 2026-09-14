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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	b, _ := io.ReadAll(resp.Body)

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
