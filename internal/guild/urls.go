package guild

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// The two links the court page shows, in the order a moderator clicks them.
//
// STEP ONE creates the server from a template. STEP TWO adds the bot. They are
// two clicks rather than one because a Discord template CANNOT CARRY A BOT: a
// template snapshots structure — channels, roles, overwrites, settings — and a
// bot is a member, so the server arrives with none. That is a platform fact, not
// an oversight in the layout, and it is the reason there is a second button at
// all. (discord/discord-api-docs#1767 asks for it; still open.)

const (
	// templateHost is Discord's own shortlink for "create a server from this
	// template". It is not an API endpoint and takes no parameters.
	templateHost = "https://discord.new/"

	// authorizeEndpoint is the consent screen.
	authorizeEndpoint = "https://discord.com/oauth2/authorize"

	// scopes: bot to add it, applications.commands so it can register slash
	// commands.
	//
	// MESSAGE_CONTENT IS CONSPICUOUSLY ABSENT and that is a decision, not an
	// omission. It is a privileged intent, it would have to be justified to
	// Discord's review, and refusing it deletes a whole class of attack: a
	// hostile guild owner controls every byte of text in their guild, so a bot
	// that reads message content in thousands of stranger-owned servers is a bot
	// whose input is attacker-chosen. Slash-command payloads still reach us, and
	// intents gate gateway events only — posting, thread and channel management
	// are REST calls and are unaffected.
	scopes = "bot applications.commands"
)

var (
	ErrNoClientID  = errors.New("guild: no Discord application client id")
	ErrNoRedirect  = errors.New("guild: no redirect uri")
	ErrRedirectTLS = errors.New("guild: redirect uri must be https")
	ErrTemplate    = errors.New("guild: template code is not 1..64 of [A-Za-z0-9]")
)

// templateCodeRe is deliberately loose on content and strict on charset.
//
// Discord does not document the code's length or alphabet, so pinning an exact
// shape would be inventing a rule that could break silently the day they change
// it. What IS worth enforcing is that nothing which could escape a URL path ever
// reaches the link — this string is concatenated onto a host and handed to a
// reader to click.
var templateCodeRe = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)

// TemplateURL is step one: the link that creates the server.
func TemplateURL(code string) (string, error) {
	if !templateCodeRe.MatchString(code) {
		return "", fmt.Errorf("%w: %q", ErrTemplate, code)
	}
	return templateHost + code, nil
}

// AuthorizeURL is step two: the consent screen that adds the bot.
//
// TWO SETTINGS LIVE IN THE DEVELOPER PORTAL AND CANNOT BE SET FROM HERE, and
// getting either wrong makes this function's output look right and behave wrong:
//
//	PUBLIC BOT must be on, or nobody but the application's owner can add it at all.
//
//	REQUIRE OAUTH2 CODE GRANT must be on. This is the one that bites. Passing
//	response_type=code and a redirect_uri is NOT sufficient by itself: for a
//	bot-scoped invite Discord ignores them unless that toggle is set, and silently
//	runs the plain invite flow instead — the bot joins, the browser never comes
//	back, no code is ever issued, and /api/guild/bound simply never fires.
//	discord/discord-api-docs#2069 asked for the parameters to work without the
//	toggle and was closed wontfix, so this is permanent, not a bug to wait out.
//
// WHY THE CODE GRANT AT ALL, when a plain invite would add the bot just as well:
// because the redirect's guild_id parameter is documented as a HINT. The
// authoritative guild identity is the nested guild object in the token-exchange
// response, and there is no token exchange without a code. A binding keyed on the
// hint is a binding an attacker can aim at a guild they do not own.
func AuthorizeURL(clientID, redirectURI string, s State) (string, error) {
	if strings.TrimSpace(clientID) == "" {
		return "", ErrNoClientID
	}
	if strings.TrimSpace(redirectURI) == "" {
		return "", ErrNoRedirect
	}
	// The redirect must be https, and not because Discord insists — it does not,
	// for localhost. It is here because the code in that redirect is a bearer
	// value good for one token exchange, and this function has no way to know it
	// is being called on a developer's laptop rather than for a link that will be
	// published on a court page.
	u, err := url.Parse(redirectURI)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNoRedirect, err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("%w: %q", ErrRedirectTLS, redirectURI)
	}
	state, err := FormatState(s)
	if err != nil {
		return "", err
	}

	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("scope", scopes)
	// RequiredBits(), not a literal: perms.go owns the set, and a second
	// spelling of it here is the drift this package is arranged to prevent.
	q.Set("permissions", RequiredBits().String())
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	// integration_type=0 is a guild install, as opposed to a user install. It is
	// explicit because the default has changed once already and a user-installed
	// clerk is a different product.
	q.Set("integration_type", "0")
	return authorizeEndpoint + "?" + q.Encode(), nil
}
