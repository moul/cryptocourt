package discordapi

import (
	"encoding/json"
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
