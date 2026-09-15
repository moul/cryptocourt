package binding

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jaekwon/kourt/internal/discordapi"
	"github.com/jaekwon/kourt/internal/gnorpc"
	"github.com/jaekwon/kourt/internal/guild"
)

// A whole bridge with both of its outside worlds faked: a Discord that will
// exchange a code for a named guild, and a chain that answers IsCourtMod however
// the test wants.
type rig struct {
	srv       *Server
	mux       *http.ServeMux
	store     *Store
	isMod     bool
	chainDown bool
	guildID   string
	logs      *bytes.Buffer
	// noInvite makes the fake guild expose no text channel, which is what a
	// withheld CREATE_INSTANT_INVITE looks like from here.
	noInvite bool
	posted   []string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	g := &rig{store: newStore(t), isMod: true, guildID: "1478455953715236886",
		logs: &bytes.Buffer{}}

	chain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.chainDown {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		out := "(false bool)"
		if g.isMod {
			out = "(true bool)"
		}
		fmt.Fprintf(w, `{"jsonrpc":"2.0","result":{"response":{"ResponseBase":{"Data":%q}}}}`,
			base64.StdEncoding.EncodeToString([]byte(out)))
	}))
	t.Cleanup(chain.Close)

	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/invites") && strings.Contains(r.URL.Path, "/guilds/"):
			fmt.Fprint(w, `[]`) // no invite exists yet
			return
		case strings.HasSuffix(r.URL.Path, "/users/@me"):
			fmt.Fprint(w, `{"id":"777"}`)
			return
		case strings.HasSuffix(r.URL.Path, "/pins") && r.Method == "GET":
			fmt.Fprint(w, `[]`)
			return
		case strings.Contains(r.URL.Path, "/messages"):
			g.posted = append(g.posted, r.URL.Path)
			fmt.Fprint(w, `{"id":"1"}`)
			return
		case strings.HasSuffix(r.URL.Path, "/channels"):
			if g.noInvite {
				fmt.Fprint(w, `[{"id":"9","type":4,"position":0}]`) // a category only
				return
			}
			fmt.Fprint(w, `[{"id":"9","type":4,"position":0},`+
				`{"id":"11","type":0,"position":2},{"id":"10","type":0,"position":1}]`)
			return
		case strings.HasSuffix(r.URL.Path, "/invites"):
			body, _ := io.ReadAll(r.Body)
			var got map[string]any
			_ = json.Unmarshal(body, &got)
			// A published link that expires is worse than no link, and a use cap
			// lets a rival take a court's listing down with throwaway accounts.
			if got["max_age"] != float64(0) || got["max_uses"] != float64(0) {
				t.Errorf("the invite is not permanent and unlimited: %v", got)
			}
			if !strings.HasSuffix(r.URL.Path, "/channels/10/invites") {
				t.Errorf("the invite was made on %s, not the first text channel by "+
					"position", r.URL.Path)
			}
			fmt.Fprint(w, `{"code":"kourtinv"}`)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if u, p, ok := r.BasicAuth(); !ok || u == "" || p == "" {
			t.Errorf("the token exchange did not authenticate: %q/%q ok=%v", u, p, ok)
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "client_secret") {
			t.Error("the client secret went in the body; it belongs in the header, " +
				"where a request log will not pick it up")
		}
		fmt.Fprintf(w, `{"access_token":"tok","token_type":"Bearer","guild":{"id":%q,"name":"A court"}}`,
			g.guildID)
	}))
	t.Cleanup(discord.Close)

	g.srv = &Server{
		Store:        g.store,
		Verifier:     &Verifier{Node: &gnorpc.Node{RPC: chain.URL, HTTP: chain.Client()}},
		Discord:      &discordapi.Client{BaseURL: discord.URL, HTTP: discord.Client()},
		ClientID:     "1234567890123456789",
		ClientSecret: "shh",
		RedirectURI:  "https://kourt.xyz/api/guild/bound",
		Log:          log.New(g.logs, "", 0),
	}
	g.mux = http.NewServeMux()
	g.srv.Routes(g.mux)
	return g
}

func (g *rig) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	rec := httptest.NewRecorder()
	g.mux.ServeHTTP(rec, req)
	return rec
}

// startFlow returns the nonce the service minted.
func (g *rig) startFlow(t *testing.T, court string) string {
	t.Helper()
	rec := g.do(t, http.MethodPost, "/api/guild/start?chain=kourt-1&court="+court, nil)
	if rec.Code != 200 {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	var out struct{ Authorize, Nonce string }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Nonce == "" {
		t.Fatal("start minted no nonce")
	}
	// The link it hands back must be one Discord will honour.
	u, err := url.Parse(out.Authorize)
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("response_type") != "code" {
		t.Error("the authorize link omits response_type=code, so the callback never fires")
	}
	return out.Nonce
}

// THE WHOLE FLOW, END TO END, with every outside answer favourable. If this ever
// fails the feature is broken regardless of what the unit tests say.
func TestAModeratorCanPublishAServer(t *testing.T) {
	g := newRig(t)
	s := newSigner(t)

	// 1. start, 2. Discord sends the reader back
	nonce := g.startFlow(t, "meta")
	rec := g.do(t, http.MethodGet,
		"/api/guild/bound?code=abc&state="+url.QueryEscape("v1:kourt-1:meta:"+nonce), nil)
	if rec.Code != 200 && rec.Code != 303 {
		t.Fatalf("bound: %d %s", rec.Code, rec.Body)
	}
	// Recorded, and pointedly not published.
	if b, _ := g.store.Listed(ctx(), "kourt-1", "meta"); b != nil {
		t.Fatalf("merely adding the bot published a server: %+v", b)
	}

	// 3. claim, with a fresh nonce — the first was spent by the redirect
	nonce2 := g.startFlow(t, "meta")
	ch, err := guild.ChallengeText(
		guild.State{Chain: "kourt-1", Court: "meta", Nonce: nonce2}, g.guildID)
	if err != nil {
		t.Fatal(err)
	}
	p := s.prove(t, ch)
	rec = g.do(t, http.MethodPost, "/api/guild/claim", ClaimRequest{
		Chain: "kourt-1", Court: "meta", GuildID: g.guildID, Nonce: nonce2,
		Address: p.Address, PubKey: p.PubKey, Signature: p.Signature,
	})
	if rec.Code != 200 {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body)
	}

	// 4. and the overlay can see it, with the disclosure attached
	rec = g.do(t, http.MethodGet, "/api/guild/servers/kourt-1/meta", nil)
	if rec.Code != 200 {
		t.Fatalf("servers: %d %s", rec.Code, rec.Body)
	}
	var out struct {
		Server *struct {
			GuildID    string `json:"guild_id"`
			Disclosure string `json:"disclosure"`
		} `json:"server"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Server == nil || out.Server.GuildID != g.guildID {
		t.Fatalf("the published server reads back as %+v", out.Server)
	}
	if !strings.Contains(out.Server.Disclosure, "does not mean") {
		t.Errorf("the listing travels without its disclosure: %q", out.Server.Disclosure)
	}
}

// THE FRONT-RUNNING CASE, which is the reason the whole gate exists. A stranger
// with a real key, a real guild and a perfectly valid signature still cannot
// publish somebody else's court.
func TestAStrangerCannotPublishACourtTheyDoNotModerate(t *testing.T) {
	g := newRig(t)
	g.isMod = false // the chain says no
	s := newSigner(t)

	nonce := g.startFlow(t, "meta")
	ch, _ := guild.ChallengeText(guild.State{Chain: "kourt-1", Court: "meta", Nonce: nonce}, g.guildID)
	p := s.prove(t, ch)
	rec := g.do(t, http.MethodPost, "/api/guild/claim", ClaimRequest{
		Chain: "kourt-1", Court: "meta", GuildID: g.guildID, Nonce: nonce,
		Address: p.Address, PubKey: p.PubKey, Signature: p.Signature,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a non-moderator got %d %s", rec.Code, rec.Body)
	}
	if b, _ := g.store.Listed(ctx(), "kourt-1", "meta"); b != nil {
		t.Errorf("a refused claim still published: %+v", b)
	}
}

// A CHAIN THAT DID NOT ANSWER IS NOT A CHAIN THAT SAID NO. Publishing on an
// answer nobody got is the one direction that must never happen, and 503 is the
// honest status: try again, nothing is decided.
func TestAnUnreachableChainRefusesRatherThanGuessing(t *testing.T) {
	g := newRig(t)
	g.chainDown = true
	s := newSigner(t)

	nonce := g.startFlow(t, "meta")
	ch, _ := guild.ChallengeText(guild.State{Chain: "kourt-1", Court: "meta", Nonce: nonce}, g.guildID)
	p := s.prove(t, ch)
	rec := g.do(t, http.MethodPost, "/api/guild/claim", ClaimRequest{
		Chain: "kourt-1", Court: "meta", GuildID: g.guildID, Nonce: nonce,
		Address: p.Address, PubKey: p.PubKey, Signature: p.Signature,
	})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("an unreachable chain gave %d %s, want 503", rec.Code, rec.Body)
	}
	if b, _ := g.store.Listed(ctx(), "kourt-1", "meta"); b != nil {
		t.Errorf("a claim published while the chain was unreachable: %+v", b)
	}
}

func TestAClaimCannotBeReplayed(t *testing.T) {
	g := newRig(t)
	s := newSigner(t)
	// The bot has to be in the guild before anything can be published in it —
	// the refusal for a guild it is not in is its own case, below.
	if err := g.store.Record(ctx(), "kourt-1", "meta", g.guildID); err != nil {
		t.Fatal(err)
	}
	nonce := g.startFlow(t, "meta")
	ch, _ := guild.ChallengeText(guild.State{Chain: "kourt-1", Court: "meta", Nonce: nonce}, g.guildID)
	p := s.prove(t, ch)
	req := ClaimRequest{
		Chain: "kourt-1", Court: "meta", GuildID: g.guildID, Nonce: nonce,
		Address: p.Address, PubKey: p.PubKey, Signature: p.Signature,
	}
	if rec := g.do(t, http.MethodPost, "/api/guild/claim", req); rec.Code != 200 {
		t.Fatalf("first claim: %d %s", rec.Code, rec.Body)
	}
	rec := g.do(t, http.MethodPost, "/api/guild/claim", req)
	if rec.Code != http.StatusConflict {
		t.Errorf("a replayed claim gave %d %s, want 409", rec.Code, rec.Body)
	}
}

// A PROOF SPENDS ITS NONCE EVEN WHEN IT FAILS. Otherwise a wrong signature is
// free to retry, and the nonce stops bounding anything.
func TestAFailedClaimStillSpendsItsNonce(t *testing.T) {
	g := newRig(t)
	mallory, victim := newSigner(t), newSigner(t)
	nonce := g.startFlow(t, "meta")
	ch, _ := guild.ChallengeText(guild.State{Chain: "kourt-1", Court: "meta", Nonce: nonce}, g.guildID)

	p := mallory.prove(t, ch)
	p.Address = victim.addr
	bad := ClaimRequest{
		Chain: "kourt-1", Court: "meta", GuildID: g.guildID, Nonce: nonce,
		Address: p.Address, PubKey: p.PubKey, Signature: p.Signature,
	}
	if rec := g.do(t, http.MethodPost, "/api/guild/claim", bad); rec.Code != http.StatusForbidden {
		t.Fatalf("a stolen-address claim gave %d", rec.Code)
	}
	// Now the same nonce, this time with an honest proof: it must be gone.
	good := mallory.prove(t, ch)
	rec := g.do(t, http.MethodPost, "/api/guild/claim", ClaimRequest{
		Chain: "kourt-1", Court: "meta", GuildID: g.guildID, Nonce: nonce,
		Address: good.Address, PubKey: good.PubKey, Signature: good.Signature,
	})
	if rec.Code != http.StatusConflict {
		t.Errorf("a nonce survived a failed attempt: %d %s", rec.Code, rec.Body)
	}
}

// THE SIGNATURE IS CHECKED AGAINST THE FLOW'S COURT, NOT THE BODY'S. A request
// that names one court in its JSON and signs for another must not slip through on
// whichever the handler happened to read second.
func TestTheBodyCannotNameACourtTheSignatureDoesNotCover(t *testing.T) {
	g := newRig(t)
	s := newSigner(t)
	nonce := g.startFlow(t, "meta") // the flow authorises meta

	// ...but the signature is over covid.
	ch, _ := guild.ChallengeText(guild.State{Chain: "kourt-1", Court: "covid", Nonce: nonce}, g.guildID)
	p := s.prove(t, ch)
	rec := g.do(t, http.MethodPost, "/api/guild/claim", ClaimRequest{
		Chain: "kourt-1", Court: "covid", GuildID: g.guildID, Nonce: nonce,
		Address: p.Address, PubKey: p.PubKey, Signature: p.Signature,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a mismatched court gave %d %s", rec.Code, rec.Body)
	}
	for _, c := range []string{"meta", "covid"} {
		if b, _ := g.store.Listed(ctx(), "kourt-1", c); b != nil {
			t.Errorf("%s was published by a mismatched claim: %+v", c, b)
		}
	}
}

// A refusal must not teach a prober which of several things was wrong.
func TestARefusalDoesNotEnumerateItsReasons(t *testing.T) {
	g := newRig(t)
	s := newSigner(t)
	nonce := g.startFlow(t, "meta")
	ch, _ := guild.ChallengeText(guild.State{Chain: "kourt-1", Court: "meta", Nonce: nonce}, g.guildID)
	p := s.prove(t, ch)
	p.Signature = base64.StdEncoding.EncodeToString([]byte("not a signature at all"))

	rec := g.do(t, http.MethodPost, "/api/guild/claim", ClaimRequest{
		Chain: "kourt-1", Court: "meta", GuildID: g.guildID, Nonce: nonce,
		Address: p.Address, PubKey: p.PubKey, Signature: p.Signature,
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("got %d", rec.Code)
	}
	body := rec.Body.String()
	for _, leak := range []string{"pubkey", "base64", "secp", "belongs to"} {
		if strings.Contains(strings.ToLower(body), leak) {
			t.Errorf("the refusal names the mechanism (%q): %s", leak, body)
		}
	}
	// ...and the detail is in the log, where an operator can still get at it.
	if !strings.Contains(g.logs.String(), "rejected") {
		t.Errorf("nothing was logged about the refusal: %q", g.logs.String())
	}
}

func TestACourtWithNoServerAnswersCleanly(t *testing.T) {
	g := newRig(t)
	rec := g.do(t, http.MethodGet, "/api/guild/servers/kourt-1/covid", nil)
	if rec.Code != 200 {
		t.Fatalf("got %d %s; a court with no Discord is the ordinary case and a 404 "+
			"on almost every page is how a real failure stops being noticeable",
			rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"server":null`) {
		t.Errorf("body = %s", rec.Body)
	}
}

func TestTheReadRouteRefusesRubbish(t *testing.T) {
	g := newRig(t)
	for _, p := range []string{
		"/api/guild/servers/", "/api/guild/servers/kourt-1",
		"/api/guild/servers/kourt-1/my-court", "/api/guild/servers/kourt-1/meta/extra",
	} {
		if rec := g.do(t, http.MethodGet, p, nil); rec.Code != http.StatusNotFound {
			t.Errorf("%s gave %d, want 404", p, rec.Code)
		}
	}
}

// start WRITES, so it must not be a GET — something will eventually prefetch a
// GET, and a link-prefetcher quietly minting flows is how a table fills up.
func TestStartRefusesAGet(t *testing.T) {
	g := newRig(t)
	if rec := g.do(t, http.MethodGet, "/api/guild/start?chain=kourt-1&court=meta", nil); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("start accepted a GET: %d", rec.Code)
	}
}

func TestBoundRefusesAStateItNeverMinted(t *testing.T) {
	g := newRig(t)
	fake := "v1:kourt-1:meta:" + strings.Repeat("ab", 24)
	rec := g.do(t, http.MethodGet, "/api/guild/bound?code=abc&state="+url.QueryEscape(fake), nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("a fabricated state gave %d %s", rec.Code, rec.Body)
	}
}

// The reader declining on Discord's own screen is a normal outcome, not a fault.
func TestBoundTreatsADeclineAsAnOutcome(t *testing.T) {
	g := newRig(t)
	rec := g.do(t, http.MethodGet, "/api/guild/bound?error=access_denied", nil)
	if rec.Code >= 500 {
		t.Errorf("declining produced %d %s", rec.Code, rec.Body)
	}
}

// A claim for a guild the bot was never added to is refused, and says which of
// the two steps was missed — this is the likeliest way a real moderator gets
// stuck, having signed correctly but skipped the invite.
func TestAClaimForAGuildTheBotIsNotInSaysSo(t *testing.T) {
	g := newRig(t)
	s := newSigner(t)
	nonce := g.startFlow(t, "meta")
	ch, _ := guild.ChallengeText(guild.State{Chain: "kourt-1", Court: "meta", Nonce: nonce}, g.guildID)
	p := s.prove(t, ch)
	rec := g.do(t, http.MethodPost, "/api/guild/claim", ClaimRequest{
		Chain: "kourt-1", Court: "meta", GuildID: g.guildID, Nonce: nonce,
		Address: p.Address, PubKey: p.PubKey, Signature: p.Signature,
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "add it first") {
		t.Errorf("the refusal does not say what to do: %s", rec.Body)
	}
}

// A GUILD ALREADY PUBLISHED FOR ANOTHER COURT IS REFUSED, not moved and not
// 500'd. Discord hands back the guild object for a bot that is already installed,
// so this path is reachable by anyone who can complete a round trip — and before
// the store refused it, the second court acquired the first court's server with
// no moderator of it signing anything.
func TestAddingAGuildThatBelongsToAnotherCourtIsRefused(t *testing.T) {
	g := newRig(t)
	s := newSigner(t)

	// meta publishes it properly.
	if err := g.store.Record(ctx(), "kourt-1", "meta", g.guildID); err != nil {
		t.Fatal(err)
	}
	nonce := g.startFlow(t, "meta")
	ch, _ := guild.ChallengeText(guild.State{Chain: "kourt-1", Court: "meta", Nonce: nonce}, g.guildID)
	p := s.prove(t, ch)
	if rec := g.do(t, http.MethodPost, "/api/guild/claim", ClaimRequest{
		Chain: "kourt-1", Court: "meta", GuildID: g.guildID, Nonce: nonce,
		Address: p.Address, PubKey: p.PubKey, Signature: p.Signature,
	}); rec.Code != 200 {
		t.Fatalf("meta could not publish: %d %s", rec.Code, rec.Body)
	}

	// Now covid runs the flow with the same guild.
	n2 := g.startFlow(t, "covid")
	rec := g.do(t, http.MethodGet,
		"/api/guild/bound?code=abc&state="+url.QueryEscape("v1:kourt-1:covid:"+n2), nil)
	if rec.Code >= 500 {
		t.Errorf("a taken guild produced a server error: %d %s", rec.Code, rec.Body)
	}
	if b, _ := g.store.Listed(ctx(), "kourt-1", "covid"); b != nil {
		t.Errorf("covid acquired meta's server: %+v", b)
	}
	if b, _ := g.store.Listed(ctx(), "kourt-1", "meta"); b == nil || b.GuildID != g.guildID {
		t.Errorf("meta lost its own server: %+v", b)
	}
}

// publish runs a complete claim and returns the response body.
func (g *rig) publish(t *testing.T, court string) *httptest.ResponseRecorder {
	t.Helper()
	s := newSigner(t)
	if err := g.store.Record(ctx(), "kourt-1", court, g.guildID); err != nil {
		t.Fatal(err)
	}
	nonce := g.startFlow(t, court)
	ch, _ := guild.ChallengeText(guild.State{Chain: "kourt-1", Court: court, Nonce: nonce}, g.guildID)
	p := s.prove(t, ch)
	return g.do(t, http.MethodPost, "/api/guild/claim", ClaimRequest{
		Chain: "kourt-1", Court: court, GuildID: g.guildID, Nonce: nonce,
		Address: p.Address, PubKey: p.PubKey, Signature: p.Signature,
	})
}

// A PUBLISHED SERVER NEEDS A DOOR. Without this the court page renders a listing
// whose button goes nowhere, which claims there is a server and then declines to
// show it.
func TestPublishingMintsTheInviteAReaderClicks(t *testing.T) {
	g := newRig(t)
	rec := g.publish(t, "meta")
	if rec.Code != 200 {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "discord.gg/kourtinv") {
		t.Errorf("the claim returned no invite: %s", rec.Body)
	}
	b, err := g.store.Listed(ctx(), "kourt-1", "meta")
	if err != nil || b == nil {
		t.Fatalf("listed: (%v, %v)", b, err)
	}
	if b.Invite != "https://discord.gg/kourtinv" {
		t.Errorf("the invite was not stored: %q", b.Invite)
	}
	// And the overlay's read carries it, because that is the only place a reader
	// ever sees it.
	rd := g.do(t, http.MethodGet, "/api/guild/servers/kourt-1/meta", nil)
	if !strings.Contains(rd.Body.String(), "discord.gg/kourtinv") {
		t.Errorf("the listing read back without its invite: %s", rd.Body)
	}
}

// A GUILD THAT WITHHELD CREATE_INSTANT_INVITE IS A REAL STATE, not a failure. The
// moderators chose the server and the chain agreed; throwing that decision away
// over a link would be the wrong trade, so the listing stands and says what is
// missing.
func TestAListingSurvivesAnInviteItCannotMint(t *testing.T) {
	g := newRig(t)
	g.noInvite = true
	rec := g.publish(t, "meta")
	if rec.Code != 200 {
		t.Fatalf("a guild with no invitable channel failed the claim: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "Create Invite") {
		t.Errorf("the response does not say what is missing or how to fix it: %s", rec.Body)
	}
	b, _ := g.store.Listed(ctx(), "kourt-1", "meta")
	if b == nil {
		t.Fatal("the listing was thrown away over a link")
	}
	if b.Invite != "" {
		t.Errorf("an invite was recorded that was never minted: %q", b.Invite)
	}
}

// The escalation from the store, reached the way an attacker would: every check
// in /claim passes, because the claimant really does moderate the court they
// named. The guild just belongs to a different one.
func TestAModeratorCannotPublishAGuildBoundToAnotherCourt(t *testing.T) {
	g := newRig(t)
	s := newSigner(t)
	// The guild was added for meta and never published.
	if err := g.store.Record(ctx(), "kourt-1", "meta", g.guildID); err != nil {
		t.Fatal(err)
	}
	// A moderator of covid claims it for covid. The chain says yes — they really
	// do moderate covid.
	nonce := g.startFlow(t, "covid")
	ch, _ := guild.ChallengeText(guild.State{Chain: "kourt-1", Court: "covid", Nonce: nonce}, g.guildID)
	p := s.prove(t, ch)
	rec := g.do(t, http.MethodPost, "/api/guild/claim", ClaimRequest{
		Chain: "kourt-1", Court: "covid", GuildID: g.guildID, Nonce: nonce,
		Address: p.Address, PubKey: p.PubKey, Signature: p.Signature,
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("got %d %s, want 404", rec.Code, rec.Body)
	}
	for _, c := range []string{"meta", "covid"} {
		if b, _ := g.store.Listed(ctx(), "kourt-1", c); b != nil {
			t.Errorf("%s was published: %+v", c, b)
		}
	}
}

// The open-flow ceiling, as a reader meets it: 429 and a sentence, not a 500.
func TestStartRefusesPolitelyAtTheCap(t *testing.T) {
	g := newRig(t)
	for i := 0; i < MaxOpenFlows; i++ {
		if rec := g.do(t, http.MethodPost, "/api/guild/start?chain=kourt-1&court=meta", nil); rec.Code != 200 {
			t.Fatalf("honest start %d: %d", i, rec.Code)
		}
	}
	rec := g.do(t, http.MethodPost, "/api/guild/start?chain=kourt-1&court=meta", nil)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("got %d %s, want 429", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "could not") {
		t.Errorf("it reads as a breakage rather than a wait: %s", rec.Body)
	}
	// Another court is unaffected.
	if rec := g.do(t, http.MethodPost, "/api/guild/start?chain=kourt-1&court=covid", nil); rec.Code != 200 {
		t.Errorf("a full meta bucket blocked covid: %d", rec.Code)
	}
}

// THE NOTICE IS POSTED IN THE ROOM ON PUBLISH. Most readers arrive through a
// shared invite and never open the court page, so the sentence saying what a
// listing does not mean has to be where they actually are.
func TestPublishingPostsTheNoticeInTheServer(t *testing.T) {
	g := newRig(t)
	if rec := g.publish(t, "meta"); rec.Code != 200 {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body)
	}
	if len(g.posted) == 0 {
		t.Error("nothing was posted in the guild")
	}
}

// THE BODY'S chain AND court ARE IGNORED, and this is the test that keeps them
// that way. A future edit reading them would undo the guarantee without touching
// anything that looks like a check, so the property is asserted rather than
// documented: a body naming a court the flow did not authorise must not publish
// that court, even when everything else about the request is honest.
func TestTheBodysChainAndCourtAreIgnored(t *testing.T) {
	g := newRig(t)
	s := newSigner(t)
	if err := g.store.Record(ctx(), "kourt-1", "meta", g.guildID); err != nil {
		t.Fatal(err)
	}
	nonce := g.startFlow(t, "meta")
	// Signed correctly for meta, which is what the flow authorised...
	ch, _ := guild.ChallengeText(guild.State{Chain: "kourt-1", Court: "meta", Nonce: nonce}, g.guildID)
	p := s.prove(t, ch)
	// ...but the body claims covid on a chain that does not exist.
	rec := g.do(t, http.MethodPost, "/api/guild/claim", ClaimRequest{
		Chain: "not-a-chain", Court: "covid", GuildID: g.guildID, Nonce: nonce,
		Address: p.Address, PubKey: p.PubKey, Signature: p.Signature,
	})
	if rec.Code != 200 {
		t.Fatalf("the body's fields changed the outcome: %d %s", rec.Code, rec.Body)
	}
	if b, _ := g.store.Listed(ctx(), "kourt-1", "covid"); b != nil {
		t.Errorf("covid was published from the body: %+v", b)
	}
	if b, _ := g.store.Listed(ctx(), "kourt-1", "meta"); b == nil {
		t.Error("meta — the court the flow authorised — was not published")
	}
}
