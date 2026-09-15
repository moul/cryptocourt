package binding

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jaekwon/kourt/internal/discordapi"
	"github.com/jaekwon/kourt/internal/guild"
)

// THE FOUR ROUTES, AND WHICH OF THEM GRANTS ANYTHING.
//
// Only one does. /claim is where a court gets a published server, and it is the
// only handler that writes StateListed. The other three mint a nonce, record a
// guild the bot happens to be in, and answer a read — none of which is a decision
// about anybody's court.
//
// That asymmetry is the design, not an accident of implementation. Anyone can
// build the authorize URL out of the client id in the page source and install the
// bot into a guild they control, so /bound is reachable by strangers and must
// leave nothing behind that matters. The gate is a signature over a nonce this
// service minted, checked against a moderator set this service does not own.
type Server struct {
	Store    *Store
	Verifier *Verifier
	Discord  *discordapi.Client

	// ClientID and ClientSecret identify the Discord application. The secret is
	// used in exactly one place — the token exchange — and never logged.
	ClientID     string
	ClientSecret string
	// RedirectURI must match the one registered with Discord, byte for byte.
	RedirectURI string
	// SiteURL is where a finished flow sends the reader back to.
	SiteURL string

	// Log is where refusals go. Nil means the standard logger.
	Log *log.Logger
}

func (s *Server) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// Routes mounts the bridge on an existing mux, the way internal/archive does.
//
// It takes the mux rather than owning one because this rides in the chat
// service's process and on its listener: a second port would be a second thing to
// proxy, a second thing to firewall, and a second thing to forget when moving
// hosts.
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/guild/start", s.start)
	mux.HandleFunc("/api/guild/bound", s.bound)
	mux.HandleFunc("/api/guild/claim", s.claim)
	mux.HandleFunc("/api/guild/servers/", s.servers)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	// nosniff, the same as internal/chat's writeJSON sets. This copy had already
	// drifted from that one without it, which is precisely the failure
	// check-addr-shapes.py's comment describes: two definitions of one thing come
	// to disagree, and nothing here was watching these two.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// start mints a flow and hands back the two links.
//
// It is a POST because it WRITES — a nonce row per call — and a GET that writes
// is a GET something will eventually prefetch. A link-prefetching browser
// extension quietly minting flows is harmless individually and is exactly the
// shape of thing that fills a table.
func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST")
		return
	}
	chain := r.URL.Query().Get("chain")
	court := r.URL.Query().Get("court")
	if chain == "" || court == "" {
		writeErr(w, http.StatusBadRequest, "chain and court are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	nonce, err := s.Store.StartFlow(ctx, chain, court)
	if errors.Is(err, ErrBadCourt) {
		writeErr(w, http.StatusBadRequest, "that is not a court slug")
		return
	}
	if errors.Is(err, ErrTooManyFlows) {
		// 429 rather than 500: nothing is broken, there are simply more
		// authorisations in flight for this court than anybody could be
		// conducting, and they expire on their own.
		s.logf("guild: %s/%s is at the open-flow cap", chain, court)
		writeErr(w, http.StatusTooManyRequests,
			"too many people are part-way through adding a server for this court — try again shortly")
		return
	}
	if err != nil {
		s.logf("guild: start flow for %s/%s: %v", chain, court, err)
		writeErr(w, http.StatusInternalServerError, "could not start")
		return
	}

	authorize, err := guild.AuthorizeURL(s.ClientID, s.RedirectURI,
		guild.State{Chain: chain, Court: court, Nonce: nonce})
	if err != nil {
		s.logf("guild: building the authorize url: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not start")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"authorize": authorize, "nonce": nonce})
}

// bound is Discord's redirect target.
//
// IT RECORDS AND NOTHING MORE. The guild is written as pending, which grants no
// provisioning, no docket and no link. The reader is then sent to the claim page,
// where the part that actually decides something happens.
//
// THE GUILD ID COMES FROM THE TOKEN EXCHANGE, NOT THE QUERY STRING. Discord's own
// documentation calls the redirect's guild_id a hint and says to require the code
// grant when you need certainty. Trusting the hint would let somebody aim a
// completed flow at a guild they do not administer.
func (s *Server) bound(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		// The reader declined on Discord's screen. That is a normal outcome and
		// should not read as a fault.
		s.redirect(w, r, "cancelled", "")
		return
	}
	code, rawState := q.Get("code"), q.Get("state")
	if code == "" || rawState == "" {
		writeErr(w, http.StatusBadRequest, "this is Discord's redirect target and needs code and state")
		return
	}
	st, err := guild.ParseState(rawState)
	if err != nil {
		s.logf("guild: unparseable state on the redirect: %v", err)
		writeErr(w, http.StatusBadRequest, "that state is not one of ours")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	// SPEND FIRST, EXCHANGE SECOND. The nonce is the cheap check and the one that
	// stops a replay; doing the network call first would mean a replayed redirect
	// costs a token exchange every time it is retried.
	flow, err := s.Store.SpendFlow(ctx, st.Nonce)
	switch {
	case errors.Is(err, ErrFlowSpent):
		s.redirect(w, r, "already-used", st.Court)
		return
	case errors.Is(err, ErrFlowStale):
		s.redirect(w, r, "expired", st.Court)
		return
	case errors.Is(err, ErrNoFlow):
		s.logf("guild: a redirect carried a nonce we never minted, court %q", st.Court)
		writeErr(w, http.StatusBadRequest, "that state is not one of ours")
		return
	case err != nil:
		s.logf("guild: spending flow: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not finish")
		return
	}

	// THE FLOW'S OWN COURT WINS over the one in the state string. They are the
	// same value in every honest request; if they ever differ, the one this
	// service stored when it minted the nonce is the one it authorised.
	if flow.Court != st.Court || flow.Chain != st.Chain {
		s.logf("guild: state said %s/%s, the minted flow said %s/%s — using the flow",
			st.Chain, st.Court, flow.Chain, flow.Court)
	}

	tok, err := s.Discord.ExchangeCode(ctx, s.ClientID, s.ClientSecret, code, s.RedirectURI)
	if errors.Is(err, discordapi.ErrNoGuildInToken) {
		s.logf("guild: token exchange returned no guild — check Require OAuth2 Code Grant")
		writeErr(w, http.StatusBadGateway, "Discord did not say which server that was")
		return
	}
	if err != nil {
		s.logf("guild: token exchange: %v", err)
		writeErr(w, http.StatusBadGateway, "Discord refused the authorisation")
		return
	}

	err = s.Store.Record(ctx, flow.Chain, flow.Court, tok.Guild.ID)
	if errors.Is(err, ErrGuildTaken) {
		// Not a fault and not a 500: the bot is in that server, but that server
		// already belongs to a different court and moving it is a decision with
		// a moderator behind it. Saying so is the difference between a reader
		// retrying forever and a reader picking another server.
		s.logf("guild: %s is already bound elsewhere, refused for %s: %v",
			tok.Guild.ID, flow.Court, err)
		s.redirect(w, r, "taken", flow.Court)
		return
	}
	if err != nil {
		s.logf("guild: recording %s for %s: %v", tok.Guild.ID, flow.Court, err)
		writeErr(w, http.StatusInternalServerError, "could not finish")
		return
	}
	s.redirect(w, r, "added", flow.Court)
}

// redirect sends the reader back to the overlay with an outcome it can render.
//
// A redirect rather than a rendered page because this service has no templates
// and should not grow any: the overlay is the thing that knows what a court page
// looks like.
func (s *Server) redirect(w http.ResponseWriter, r *http.Request, outcome, court string) {
	site := s.SiteURL
	if site == "" {
		writeJSON(w, http.StatusOK, map[string]string{"outcome": outcome, "court": court})
		return
	}
	dest := strings.TrimSuffix(site, "/") + "/#/c/" + court + "?discord=" + outcome
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// ClaimRequest is the proof a moderator sends to publish a server.
//
// CHAIN AND COURT ARE ACCEPTED AND IGNORED, deliberately. The handler takes both
// from the flow the nonce was minted against, never from the body — a request
// that names one court in its JSON and signs for another must not be resolved in
// favour of whichever field was read second. They stay in the struct because
// clients send them and a stricter decoder would reject honest requests, and they
// are called out here because a future edit that starts reading them would undo
// the guarantee without touching anything that looks like a check.
type ClaimRequest struct {
	Chain     string `json:"chain"` // ignored; see above
	Court     string `json:"court"` // ignored; see above
	GuildID   string `json:"guild_id"`
	Nonce     string `json:"nonce"`
	Address   string `json:"address"`
	PubKey    string `json:"pubkey"`
	Signature string `json:"signature"`
}

// claim is the only handler that publishes anything.
//
// THE ORDER OF ITS THREE CHECKS IS THE SECURITY OF THE WHOLE FEATURE:
//
//  1. the nonce was minted here and has not been spent — this is ours, and fresh
//  2. the signature covers the challenge naming this court, this guild, this
//     nonce — the sender controls the address they claim
//  3. the chain says that address moderates that court — they are allowed to
//
// Each is necessary and none implies another. Dropping 1 makes a signature a
// reusable warrant; dropping 2 lets anyone claim any address; dropping 3 lets any
// stranger publish a server for a court they have nothing to do with, which is
// the front-running this design exists to prevent.
func (s *Server) claim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST")
		return
	}
	var req ClaimRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "that is not a claim")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	// (1) The nonce. Spent here, so a claim cannot be retried with the same one
	// even if everything after this fails — a proof is good for one attempt.
	flow, err := s.Store.SpendFlow(ctx, req.Nonce)
	switch {
	case errors.Is(err, ErrFlowSpent):
		writeErr(w, http.StatusConflict, "that claim was already used — start again")
		return
	case errors.Is(err, ErrFlowStale):
		writeErr(w, http.StatusGone, "that claim expired — start again")
		return
	case errors.Is(err, ErrNoFlow):
		writeErr(w, http.StatusBadRequest, "that claim is not one of ours")
		return
	case err != nil:
		s.logf("guild: claim, spending flow: %v", err)
		writeErr(w, http.StatusInternalServerError, "could not finish")
		return
	}

	// (2) The signature, over the challenge the FLOW describes rather than the
	// one the request describes. Building it from stored values is what stops a
	// request naming one court in its body and another in its challenge.
	challenge, err := guild.ChallengeText(
		guild.State{Chain: flow.Chain, Court: flow.Court, Nonce: req.Nonce}, req.GuildID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "that guild id is not a Discord server")
		return
	}
	signer, err := VerifyProof(challenge, Proof{
		Address: req.Address, PubKey: req.PubKey, Signature: req.Signature,
	})
	if err != nil {
		// Deliberately vague to the caller, specific in the log: which of the
		// several ways a proof can be wrong is not something a prober should be
		// able to enumerate for free.
		s.logf("guild: claim for %s rejected: %v", flow.Court, err)
		writeErr(w, http.StatusForbidden, "that signature does not check out")
		return
	}

	// (3) The chain. An error here is NOT a refusal — it means nobody knows, and
	// publishing on an answer nobody got is the one direction that must not
	// happen. See Verifier.IsMod.
	isMod, err := s.Verifier.IsMod(ctx, flow.Court, signer)
	if errors.Is(err, ErrNoCourt) {
		writeErr(w, http.StatusNotFound, "no such court on this chain")
		return
	}
	if err != nil {
		s.logf("guild: claim for %s, asking the chain: %v", flow.Court, err)
		writeErr(w, http.StatusServiceUnavailable, "could not reach the chain to check — try again shortly")
		return
	}
	if !isMod {
		writeErr(w, http.StatusForbidden,
			"that address does not moderate this court, so it cannot publish its server")
		return
	}

	if err := s.Store.List(ctx, flow.Chain, flow.Court, req.GuildID, signer); errors.Is(err, ErrNoBinding) {
		writeErr(w, http.StatusNotFound, "the bot is not in that server — add it first")
		return
	} else if err != nil {
		s.logf("guild: listing %s for %s: %v", req.GuildID, flow.Court, err)
		writeErr(w, http.StatusInternalServerError, "could not finish")
		return
	}
	s.logf("guild: %s published %s for %s/%s", signer, req.GuildID, flow.Chain, flow.Court)

	// THE INVITE IS MINTED AFTER THE PUBLISH AND CANNOT UNDO IT. A guild that
	// granted the listing but withheld CREATE_INSTANT_INVITE is a real state: the
	// moderators chose the server and the bot cannot yet produce a door to it.
	// Failing the claim over that would throw away a decision that was correctly
	// made, so the listing stands and the link arrives when it can.
	// THE NOTICE GOES IN THE ROOM, and its failure is not the claim's failure
	// either. Most readers will arrive through a shared invite and never see the
	// court page, so the sentence that says what a listing does not mean has to
	// be where they are. The guild's owner can unpin or delete it — their guild —
	// which is why this is legibility rather than a guarantee, and why losing a
	// correct publish over it would be the wrong trade.
	if err := s.Discord.EnsureDisclosure(req.GuildID, flow.Chain, flow.Court, s.SiteURL); err != nil {
		s.logf("guild: listed %s but could not post the notice in it: %v", req.GuildID, err)
	}

	invite, err := s.Discord.EnsureInvite(req.GuildID)
	if err != nil {
		s.logf("guild: listed %s but could not mint an invite: %v", req.GuildID, err)
		writeJSON(w, http.StatusOK, map[string]string{
			"state": StateListed, "guild_id": req.GuildID,
			"warning": "the server is published, but the bot could not create an invite — " +
				"check it still has Create Invite in that server",
		})
		return
	}
	if err := s.Store.SetInvite(ctx, flow.Chain, flow.Court, req.GuildID, invite); err != nil {
		// REPORTING THE INVITE HERE WOULD BE A LIE THE PAGE THEN MISATTRIBUTES.
		// The claimant would be told a link that the next read cannot produce,
		// and the overlay's empty-invite branch says "it has not been given
		// permission to create an invite" — blaming Discord for a database
		// error. Saying the true thing is cheaper than a wrong diagnosis.
		s.logf("guild: minted an invite for %s but could not store it: %v", req.GuildID, err)
		writeJSON(w, http.StatusOK, map[string]string{
			"state": StateListed, "guild_id": req.GuildID,
			"warning": "the server is published, but its invite could not be saved — " +
				"it will be minted again on the next publish",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"state": StateListed, "guild_id": req.GuildID, "invite": invite})
}

// servers answers /api/guild/servers/{chain}/{court} — what the overlay reads.
//
// It returns 200 with a null binding rather than 404 for a court with no server,
// because "this court has no Discord" is the ordinary case for almost every
// court, and a 404 in the console on almost every page is how a real failure
// stops being noticeable.
func (s *Server) servers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "GET")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/guild/servers/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeErr(w, http.StatusNotFound, "/api/guild/servers/{chain}/{court}")
		return
	}
	chain, court := parts[0], parts[1]
	if !guild.ValidCourt(court) {
		writeErr(w, http.StatusNotFound, "that is not a court slug")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	b, err := s.Store.Listed(ctx, chain, court)
	if err != nil {
		s.logf("guild: reading the listing for %s/%s: %v", chain, court, err)
		writeErr(w, http.StatusInternalServerError, "could not read")
		return
	}
	if b == nil {
		writeJSON(w, http.StatusOK, map[string]any{"server": nil})
		return
	}
	// THE INVITE, NOT THE GUILD ID, is what a reader can use — and the disclosure
	// travels with it. A caller that renders the link without the sentence is
	// making a claim this service did not make.
	writeJSON(w, http.StatusOK, map[string]any{
		"server": map[string]any{
			"guild_id":  b.GuildID,
			"invite":    b.Invite,
			"listed_at": b.ListedAt.Unix(),
			"disclosure": "Listed means this court's current moderators chose this server. " +
				"It does not mean the court is legitimate, or that anything said there is true.",
		},
	})
}
