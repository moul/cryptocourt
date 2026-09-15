package discordapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaekwon/kourt/internal/guild"
)

// A recording Discord. Every test here asserts on what was SENT, because that is
// the half a live run cannot show you afterwards: a guild ends up looking roughly
// right whether or not the parent ids, the overwrite ids and the permission
// strings were the ones intended.
type fake struct {
	t      *testing.T
	calls  []call
	nextID int
	// handler lets one test override a single route; nil means the ordinary
	// create-everything behaviour.
	handler func(w http.ResponseWriter, r *http.Request) bool
}

type call struct {
	Method string
	Path   string
	Body   map[string]any
}

func newFake(t *testing.T) (*fake, *Client) {
	t.Helper()
	f := &fake{t: t, nextID: 100}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, &Client{Token: "test-token", BaseURL: srv.URL, HTTP: srv.Client()}
}

func (f *fake) serve(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	f.calls = append(f.calls, call{Method: r.Method, Path: r.URL.Path, Body: body})

	if f.handler != nil && f.handler(w, r) {
		return
	}
	f.nextID++
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id":"%d","code":"tpl%d"}`, f.nextID, f.nextID)
}

func (f *fake) creates() []call {
	var out []call
	for _, c := range f.calls {
		if c.Method == "POST" && strings.Contains(c.Path, "/guilds/") {
			out = append(out, c)
		}
	}
	return out
}

// THE AUTHORIZATION HEADER IS THE ONE THING EVERY CALL MUST CARRY, and a client
// that silently omits it fails with 401s that read like a bad token.
func TestEveryRequestIsAuthenticatedAsABot(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		if r.Header.Get("User-Agent") == "" {
			t.Error("no User-Agent; Discord enforces one on some routes")
		}
		fmt.Fprint(w, `{"id":"1"}`)
	}))
	defer srv.Close()

	c := &Client{Token: "abc", BaseURL: srv.URL, HTTP: srv.Client()}
	if err := c.Do("GET", "/x", nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "Bot abc" {
		t.Errorf("Authorization = %v, want [Bot abc]", seen)
	}
}

// ORDER IS THE INVARIANT THAT FAILS SILENTLY. Discord accepts a channel naming a
// parent that does not exist by ignoring the parent, so a wrong order produces an
// empty category and a flat server rather than an error.
func TestApplyLayoutCreatesRolesFirstAndCategoriesBeforeTheirChildren(t *testing.T) {
	f, c := newFake(t)
	if err := c.ApplyLayout("999", guild.CourtLayout(), nil); err != nil {
		t.Fatal(err)
	}

	var kinds []string
	for _, cl := range f.creates() {
		switch {
		case strings.HasSuffix(cl.Path, "/roles"):
			kinds = append(kinds, "role:"+str(cl.Body["name"]))
		case strings.HasSuffix(cl.Path, "/channels"):
			kinds = append(kinds, "chan:"+str(cl.Body["name"]))
		}
	}
	if len(kinds) < 7 {
		t.Fatalf("only %d creates: %v", len(kinds), kinds)
	}
	firstChan := -1
	for i, k := range kinds {
		if strings.HasPrefix(k, "chan:") && firstChan < 0 {
			firstChan = i
		}
		if strings.HasPrefix(k, "role:") && firstChan >= 0 {
			t.Errorf("role created after a channel: %v", kinds)
		}
	}
	// The category must precede everything that names it.
	catAt := indexOf(kinds, "chan:the court")
	if catAt < 0 {
		t.Fatalf("the category was never created: %v", kinds)
	}
	for _, child := range []string{"chan:docket", "chan:claims", "chan:how-this-works"} {
		if i := indexOf(kinds, child); i < catAt {
			t.Errorf("%s created at %d, before its parent at %d", child, i, catAt)
		}
	}
}

// The ids have to be substituted, not the names — this is the part that cannot be
// checked by looking at a finished guild, because a dropped parent_id looks
// identical to a category somebody emptied by hand.
func TestChildChannelsCarryTheirParentsRealId(t *testing.T) {
	f, c := newFake(t)
	if err := c.ApplyLayout("999", guild.CourtLayout(), nil); err != nil {
		t.Fatal(err)
	}
	// Every channel the layout parents must send a parent_id, and it must not be
	// the category's NAME — sending the name is the mistake that looks correct in
	// the request body and silently produces a flat server.
	for _, cl := range f.creates() {
		if !strings.HasSuffix(cl.Path, "/channels") {
			continue
		}
		name := str(cl.Body["name"])
		if name == "the court" || name == "lobby" {
			continue // top level by design
		}
		pid, ok := cl.Body["parent_id"]
		if !ok {
			t.Errorf("channel %q sent no parent_id", name)
			continue
		}
		if str(pid) == "the court" {
			t.Errorf("channel %q sent the category NAME as parent_id", name)
		}
	}
}

// @everyone's id is the guild id. Getting this wrong means the default-deny lands
// on nothing and the docket is world-writable.
func TestTheEveryoneOverwriteUsesTheGuildId(t *testing.T) {
	f, c := newFake(t)
	if err := c.ApplyLayout("42424242", guild.CourtLayout(), nil); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, cl := range f.creates() {
		ows, ok := cl.Body["permission_overwrites"].([]any)
		if !ok {
			continue
		}
		for _, raw := range ows {
			o := raw.(map[string]any)
			if str(o["id"]) == "42424242" {
				found = true
				if str(o["deny"]) == "0" && str(o["allow"]) == "0" {
					t.Errorf("the @everyone overwrite on %q grants and denies nothing",
						str(cl.Body["name"]))
				}
			}
		}
	}
	if !found {
		t.Error("no overwrite used the guild id, so @everyone was never targeted")
	}
}

// Permissions go out as decimal STRINGS. Sending numbers is the failure that
// shows up as silently-wrong permissions, because the value outgrew the integer a
// JavaScript client holds exactly.
func TestPermissionsAreSentAsStrings(t *testing.T) {
	f, c := newFake(t)
	if err := c.ApplyLayout("999", guild.CourtLayout(), nil); err != nil {
		t.Fatal(err)
	}
	var checked int
	for _, cl := range f.creates() {
		if p, ok := cl.Body["permissions"]; ok {
			if _, isStr := p.(string); !isStr {
				t.Errorf("role %q sent permissions as %T", str(cl.Body["name"]), p)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no role carried a permissions field; this test measured nothing")
	}
}

// 30031 IS AN INSTRUCTION, NOT A FAILURE: a guild holds one template, so "already
// has one" means sync it. Treating it as an error would mean the template every
// court server is copied from could never be updated.
func TestEnsureTemplateSyncsWhenTheGuildAlreadyHasOne(t *testing.T) {
	f, c := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/templates") {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"code":%d,"message":"Guild already has a template."}`, codeTemplateExists)
			return true
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/templates") {
			fmt.Fprint(w, `[{"code":"existing1"}]`)
			return true
		}
		return false
	}

	code, synced, err := c.EnsureTemplate("999", "Kourt court", "desc")
	if err != nil {
		t.Fatalf("a guild with an existing template errored: %v", err)
	}
	if !synced {
		t.Error("synced = false; the caller cannot tell it did not get a fresh code")
	}
	if code != "existing1" {
		t.Errorf("code = %q, want existing1", code)
	}
	var sawPut bool
	for _, cl := range f.calls {
		if cl.Method == "PUT" && strings.HasSuffix(cl.Path, "/templates/existing1") {
			sawPut = true
		}
	}
	if !sawPut {
		t.Error("no PUT was sent, so the existing template was never synced to the " +
			"guild's current state — which is the whole point of re-running this")
	}
}

func TestEnsureTemplateReportsAFreshCodeAsFresh(t *testing.T) {
	_, c := newFake(t)
	code, synced, err := c.EnsureTemplate("999", "Kourt court", "desc")
	if err != nil {
		t.Fatal(err)
	}
	if synced {
		t.Error("a first-time mint reported itself as a sync")
	}
	if code == "" {
		t.Error("no code returned")
	}
}

// An ordinary error must still carry Discord's body: the HTTP status alone hides
// which of a dozen things went wrong, and the code is the part that is lookupable.
func TestAnErrorEchoesDiscordsOwnMessage(t *testing.T) {
	_, c := newFake(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"code":50013,"message":"Missing Permissions"}`)
	}))
	defer srv.Close()
	c.BaseURL = srv.URL

	err := c.Do("POST", "/guilds/1/roles", map[string]any{}, nil)
	if err == nil {
		t.Fatal("a 403 was not an error")
	}
	if !strings.Contains(err.Error(), "Missing Permissions") || !strings.Contains(err.Error(), "50013") {
		t.Errorf("the error hides Discord's own message: %v", err)
	}
}

// A half-applied layout is worse than none: the next run creates the roles a
// second time. So a failure partway must surface, not be swallowed.
func TestApplyLayoutStopsAtTheFirstFailure(t *testing.T) {
	f, c := newFake(t)
	f.handler = func(w http.ResponseWriter, r *http.Request) bool {
		if strings.HasSuffix(r.URL.Path, "/channels") {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"code":50013,"message":"Missing Permissions"}`)
			return true
		}
		return false
	}
	err := c.ApplyLayout("999", guild.CourtLayout(), nil)
	if err == nil {
		t.Fatal("a refused channel creation was not reported")
	}
	if !strings.Contains(err.Error(), "creating channel") {
		t.Errorf("the error does not say what failed: %v", err)
	}
	// It must have stopped, not carried on through the rest of the layout.
	var chanAttempts int
	for _, cl := range f.calls {
		if strings.HasSuffix(cl.Path, "/channels") {
			chanAttempts++
		}
	}
	if chanAttempts != 1 {
		t.Errorf("kept going after a failure: %d channel attempts", chanAttempts)
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func indexOf(xs []string, want string) int {
	for i, x := range xs {
		if x == want {
			return i
		}
	}
	return -1
}

// REUSE BEFORE CREATE. Re-running a publish must not leave a guild carrying a
// dozen equivalent invites, and a link already published somewhere should keep
// working rather than being superseded by a fresh one.
func TestAnExistingPermanentInviteIsReused(t *testing.T) {
	var posted int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posted++
		}
		if strings.HasSuffix(r.URL.Path, "/guilds/1/invites") {
			fmt.Fprint(w, `[{"code":"old","max_age":0,"max_uses":0}]`)
			return
		}
		fmt.Fprint(w, `{"code":"new"}`)
	}))
	defer srv.Close()
	c := &Client{Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}

	got, err := c.EnsureInvite("1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://discord.gg/old" {
		t.Errorf("got %q, want the existing invite", got)
	}
	if posted != 0 {
		t.Errorf("%d invite(s) created despite a usable one existing", posted)
	}
}

// An invite that expires or is capped is NOT usable for this: the page would keep
// claiming a server after the link died, and a use cap lets a rival exhaust it.
func TestAnExpiringOrCappedInviteIsNotReused(t *testing.T) {
	for name, existing := range map[string]string{
		"expiring":  `[{"code":"old","max_age":86400,"max_uses":0}]`,
		"capped":    `[{"code":"old","max_age":0,"max_uses":25}]`,
		"temporary": `[{"code":"old","max_age":0,"max_uses":0,"temporary":true}]`,
		"revoked":   `[{"code":"old","max_age":0,"max_uses":0,"revoked":true}]`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/guilds/1/invites"):
				fmt.Fprint(w, existing)
			case strings.HasSuffix(r.URL.Path, "/channels"):
				fmt.Fprint(w, `[{"id":"5","type":0,"position":0}]`)
			default:
				fmt.Fprint(w, `{"code":"fresh"}`)
			}
		}))
		got, err := (&Client{Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}).EnsureInvite("1")
		srv.Close()
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != "https://discord.gg/fresh" {
			t.Errorf("%s: reused %q, which does not last", name, got)
		}
	}
}

// A guild the bot cannot list invites for is not a guild it cannot invite to:
// MANAGE_GUILD reads that list and may be withheld while CREATE_INSTANT_INVITE is
// granted. The read failing must fall through to creating one.
func TestAnUnreadableInviteListStillMintsOne(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/guilds/1/invites"):
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"code":50013,"message":"Missing Permissions"}`)
		case strings.HasSuffix(r.URL.Path, "/channels"):
			fmt.Fprint(w, `[{"id":"5","type":0,"position":0}]`)
		default:
			fmt.Fprint(w, `{"code":"fresh"}`)
		}
	}))
	defer srv.Close()
	got, err := (&Client{Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}).EnsureInvite("1")
	if err != nil {
		t.Fatalf("an unreadable invite list stopped the mint: %v", err)
	}
	if got != "https://discord.gg/fresh" {
		t.Errorf("got %q", got)
	}
}

func TestAGuildWithNoTextChannelSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/guilds/1/invites"):
			fmt.Fprint(w, `[]`)
		case strings.HasSuffix(r.URL.Path, "/channels"):
			// a category and a forum: neither is somewhere to land
			fmt.Fprint(w, `[{"id":"4","type":4,"position":0},{"id":"7","type":15,"position":1}]`)
		default:
			t.Errorf("it tried to create an invite on %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	_, err := (&Client{Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}).EnsureInvite("1")
	if !errors.Is(err, ErrNoInvitableChannel) {
		t.Errorf("got %v, want ErrNoInvitableChannel", err)
	}
}

// THE NOTICE HAS TO SAY THE SAME THING THE COURT PAGE SAYS. A reader who checks
// both and finds two accounts of what a listing means has been given a reason to
// trust neither. check-guild-copy.py holds the site's two copies together; this
// is the third.
func TestTheDisclosureQuotesTheSameClauseAsTheSite(t *testing.T) {
	got := disclosureText("kourt-1", "meta", "https://kourt.xyz", "999")
	for _, must := range []string{
		"current moderators chose this server",
		"does not mean the court is legitimate",
		"anything said here is true",
	} {
		if !strings.Contains(got, must) {
			t.Errorf("the notice does not say %q:\n%s", must, got)
		}
	}
}

// A reader can only tell the clerk apart by its id: an owner controls every
// nickname, avatar and webhook in their own guild.
func TestTheDisclosureNamesTheBotById(t *testing.T) {
	got := disclosureText("kourt-1", "meta", "https://kourt.xyz", "424242")
	if !strings.Contains(got, "<@424242>") {
		t.Errorf("the notice does not cite the bot's id:\n%s", got)
	}
	if !strings.Contains(got, "choose a name and a picture") {
		t.Errorf("the notice does not say why the id is what matters:\n%s", got)
	}
}

func TestTheDisclosureLinksBackToTheCourt(t *testing.T) {
	got := disclosureText("kourt-1", "meta", "https://kourt.xyz", "1")
	if !strings.Contains(got, "https://kourt.xyz/#/c/meta") {
		t.Errorf("bad court link:\n%s", got)
	}
	// A trailing slash on the configured site must not double up.
	if s := disclosureText("kourt-1", "meta", "https://kourt.xyz/", "1"); strings.Contains(s, "xyz//") {
		t.Errorf("a trailing slash doubled: %s", s)
	}
	// And the marker the bot recognises its own post by is really in the text.
	if !strings.Contains(got, disclosureMark) {
		t.Errorf("the notice does not carry the marker that identifies it later")
	}
}

// IDEMPOTENT BY REWRITING ITS OWN PIN. A bot that posts a second notice every
// sweep is a bot spamming the room it is trying to be trustworthy in.
func TestTheDisclosureIsNotPostedTwice(t *testing.T) {
	var posts, patches int
	body := disclosureText("kourt-1", "meta", "https://kourt.xyz", "77")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/users/@me"):
			fmt.Fprint(w, `{"id":"77"}`)
		case strings.HasSuffix(r.URL.Path, "/channels"):
			fmt.Fprint(w, `[{"id":"5","type":0,"position":0,"name":"how-this-works"}]`)
		case strings.HasSuffix(r.URL.Path, "/pins") && r.Method == "GET":
			fmt.Fprintf(w, `[{"id":"9","author":{"id":"77"},"content":%q}]`, body)
		case r.Method == "POST":
			posts++
			fmt.Fprint(w, `{"id":"10"}`)
		case r.Method == "PATCH":
			patches++
			fmt.Fprint(w, `{}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer srv.Close()
	c := &Client{Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}

	for i := 0; i < 3; i++ {
		if err := c.EnsureDisclosure("1", "kourt-1", "meta", "https://kourt.xyz"); err != nil {
			t.Fatal(err)
		}
	}
	if posts != 0 {
		t.Errorf("it posted %d new notice(s) over an identical existing pin", posts)
	}
	if patches != 0 {
		t.Errorf("it rewrote an already-correct notice %d time(s)", patches)
	}
}

// A STALE NOTICE IS REWRITTEN RATHER THAN DUPLICATED — the court's name or the
// site can change, and the room should end up with one correct notice either way.
func TestAStaleDisclosureIsRewrittenInPlace(t *testing.T) {
	var posts, patches int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/users/@me"):
			fmt.Fprint(w, `{"id":"77"}`)
		case strings.HasSuffix(r.URL.Path, "/channels"):
			fmt.Fprint(w, `[{"id":"5","type":0,"position":0,"name":"how-this-works"}]`)
		case strings.HasSuffix(r.URL.Path, "/pins") && r.Method == "GET":
			fmt.Fprint(w, `[{"id":"9","author":{"id":"77"},"content":"old text /#/c/meta stale"}]`)
		case r.Method == "POST":
			posts++
			fmt.Fprint(w, `{"id":"10"}`)
		case r.Method == "PATCH":
			patches++
			fmt.Fprint(w, `{}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer srv.Close()
	c := &Client{Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}
	if err := c.EnsureDisclosure("1", "kourt-1", "meta", "https://kourt.xyz"); err != nil {
		t.Fatal(err)
	}
	if patches != 1 || posts != 0 {
		t.Errorf("stale notice: %d patch(es), %d post(s); want 1 and 0", patches, posts)
	}
}

// SOMEBODY ELSE'S PINNED MESSAGE IS NOT THE CLERK'S, however much it looks like
// it. The author id is the only thing that distinguishes them.
func TestAnImitationPinIsNotMistakenForOurs(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/users/@me"):
			fmt.Fprint(w, `{"id":"77"}`)
		case strings.HasSuffix(r.URL.Path, "/channels"):
			fmt.Fprint(w, `[{"id":"5","type":0,"position":0,"name":"how-this-works"}]`)
		case strings.HasSuffix(r.URL.Path, "/pins") && r.Method == "GET":
			// A perfect copy of our text, posted by somebody else.
			fmt.Fprintf(w, `[{"id":"9","author":{"id":"66"},"content":%q}]`,
				disclosureText("kourt-1", "meta", "https://kourt.xyz", "77"))
		case r.Method == "POST":
			posts++
			fmt.Fprint(w, `{"id":"10"}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer srv.Close()
	c := &Client{Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}
	if err := c.EnsureDisclosure("1", "kourt-1", "meta", "https://kourt.xyz"); err != nil {
		t.Fatal(err)
	}
	if posts != 1 {
		t.Errorf("it treated a stranger's copy as its own and posted %d notice(s)", posts)
	}
}

// Unreadable pins must not skip the notice: MANAGE_MESSAGES reads that list and
// may be withheld while sending is allowed.
func TestUnreadablePinsStillGetANotice(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/users/@me"):
			fmt.Fprint(w, `{"id":"77"}`)
		case strings.HasSuffix(r.URL.Path, "/channels"):
			fmt.Fprint(w, `[{"id":"5","type":0,"position":0,"name":"lobby"}]`)
		case strings.HasSuffix(r.URL.Path, "/pins") && r.Method == "GET":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"code":50013,"message":"Missing Permissions"}`)
		case r.Method == "POST":
			posts++
			fmt.Fprint(w, `{"id":"10"}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer srv.Close()
	c := &Client{Token: "t", BaseURL: srv.URL, HTTP: srv.Client()}
	if err := c.EnsureDisclosure("1", "kourt-1", "meta", "https://kourt.xyz"); err != nil {
		t.Fatal(err)
	}
	if posts != 1 {
		t.Errorf("unreadable pins skipped the notice (%d posts)", posts)
	}
}
