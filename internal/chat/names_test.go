package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
	"time"
)

// The four rules a display name now passes through, in the order the handler
// applies them:
//
//	1. reserved  — nobody, ever, no token   (clerk, admin, …)
//	2. owner     — one person, with a token (jae, jaekwon)
//	3. held      — first-come per court, expiring
//	4. free
//
// The ORDER is the part worth testing. Rules 1 and 2 look similar and mean
// opposite things: a token that unlocked "clerk" would hand the room's own
// voice to a person, which is the impersonation the clerk name exists to stop.

func ownerSrv(t *testing.T, token string) (*Server, *Store, *time.Time) {
	t.Helper()
	srv, st, clk := newServer(t)
	if token != "" {
		sum := sha256.Sum256([]byte(token))
		srv.OwnerTokenSHA256 = hex.EncodeToString(sum[:])
	}
	srv.NameHold = 24 * time.Hour
	return srv, st, clk
}

func TestReservedNamesAreRefusedToEveryone(t *testing.T) {
	srv, _, _ := ownerSrv(t, "s3cret")
	for _, name := range []string{
		"clerk", "Clerk", "CLERK", "c1erk", "admin", "Admin", "ADMIN",
		"administrator", "mod", "moderator", "system", "root", "owner",
		"support", "official", "staff", "kourt",
	} {
		rec := do(t, srv, postReq(t, "/api/chat/dev/bedford", name, "hello"))
		if rec.Code != http.StatusConflict {
			t.Errorf("%q: got %d, want 409", name, rec.Code)
		}
	}
}

// The homoglyph fold is the reason IsReservedName compares skeletons, and a
// Cyrillic а in "admin" is the cheapest possible attack on a plain ==.
func TestReservedNamesRefuseHomoglyphs(t *testing.T) {
	srv, _, _ := ownerSrv(t, "s3cret")
	for _, name := range []string{
		"аdmin", // Cyrillic а
		"cIerk", // capital I for l
		"сlerk", // Cyrillic с
		"mοd",   // Greek omicron
	} {
		rec := do(t, srv, postReq(t, "/api/chat/dev/bedford", name, "hello"))
		if rec.Code != http.StatusConflict {
			t.Errorf("%q: got %d, want 409 — the skeleton fold should catch this", name, rec.Code)
		}
	}
}

// THE ONE THAT MATTERS MOST. A token grants a person their name; it must never
// grant a role. If this ever passes 200 the operator can speak as the room.
func TestOwnerTokenDoesNotUnlockReservedNames(t *testing.T) {
	srv, _, _ := ownerSrv(t, "s3cret")
	for _, name := range []string{"clerk", "admin", "system"} {
		r := postReq(t, "/api/chat/dev/bedford", name, "hello")
		r.Header.Set("X-Kourt-Owner", "s3cret")
		if rec := do(t, srv, r); rec.Code != http.StatusConflict {
			t.Errorf("%q with a valid owner token: got %d, want 409", name, rec.Code)
		}
	}
}

func TestOwnerNamesNeedTheToken(t *testing.T) {
	srv, _, _ := ownerSrv(t, "s3cret")
	for _, name := range []string{"jae", "jaekwon", "Jae", "JAEKWON", "jаe"} {
		if rec := do(t, srv, postReq(t, "/api/chat/dev/bedford", name, "hi")); rec.Code != http.StatusConflict {
			t.Errorf("%q without a token: got %d, want 409", name, rec.Code)
		}
	}
	r := postReq(t, "/api/chat/dev/bedford", "jae", "hi")
	r.Header.Set("X-Kourt-Owner", "s3cret")
	if rec := do(t, srv, r); rec.Code != http.StatusOK {
		t.Fatalf("jae with the right token: got %d %s, want 200", rec.Code, rec.Body)
	}
	r2 := postReq(t, "/api/chat/dev/bedford", "jaekwon", "hi again")
	r2.Header.Set("X-Kourt-Owner", "wrong")
	if rec := do(t, srv, r2); rec.Code != http.StatusConflict {
		t.Fatalf("jaekwon with a wrong token: got %d, want 409", rec.Code)
	}
}

// An unconfigured server holds the names shut rather than falling open. A
// deployment that never set a token must not be one where anyone can be "jae".
func TestOwnerNamesRefusedWhenNoTokenConfigured(t *testing.T) {
	srv, _, _ := newServer(t)
	srv.NameHold = 24 * time.Hour
	r := postReq(t, "/api/chat/dev/bedford", "jae", "hi")
	r.Header.Set("X-Kourt-Owner", "anything")
	if rec := do(t, srv, r); rec.Code != http.StatusConflict {
		t.Fatalf("unconfigured server: got %d, want 409", rec.Code)
	}
}

func TestNameHeldByAnotherAuthorIsRefused(t *testing.T) {
	srv, _, clk := ownerSrv(t, "")
	if rec := do(t, srv, postReq(t, "/api/chat/dev/bedford", "alice", "first")); rec.Code != 200 {
		t.Fatalf("alice's first post: %d %s", rec.Code, rec.Body)
	}
	// Past the per-address throttle, which is a different rule and would
	// otherwise answer 429 before the name check is ever reached.
	*clk = clk.Add(time.Minute)
	// Same author, same name: still theirs.
	if rec := do(t, srv, postReq(t, "/api/chat/dev/bedford", "alice", "second")); rec.Code != 200 {
		t.Fatalf("alice again: %d %s", rec.Code, rec.Body)
	}
	// A different address wearing it is refused.
	r := postReq(t, "/api/chat/dev/bedford", "alice", "impostor")
	r.RemoteAddr = "198.51.100.7:1234"
	if rec := do(t, srv, r); rec.Code != http.StatusConflict {
		t.Fatalf("a stranger taking alice: got %d, want 409", rec.Code)
	}
	// And so is a homoglyph of it.
	r2 := postReq(t, "/api/chat/dev/bedford", "аlice", "impostor")
	r2.RemoteAddr = "198.51.100.7:1234"
	if rec := do(t, srv, r2); rec.Code != http.StatusConflict {
		t.Fatalf("a stranger taking а-lice: got %d, want 409", rec.Code)
	}
}

// SCOPED TO THE COURT. The same word in another room is another stranger.
func TestNameHoldIsPerCourt(t *testing.T) {
	srv, _, _ := ownerSrv(t, "")
	if rec := do(t, srv, postReq(t, "/api/chat/dev/bedford", "alice", "here")); rec.Code != 200 {
		t.Fatalf("bedford: %d %s", rec.Code, rec.Body)
	}
	r := postReq(t, "/api/chat/dev/covid", "alice", "elsewhere")
	r.RemoteAddr = "198.51.100.7:1234"
	if rec := do(t, srv, r); rec.Code != http.StatusOK {
		t.Fatalf("another court should be free: got %d %s", rec.Code, rec.Body)
	}
}

// AND IT EXPIRES, because an ip_hash is not a person and a permanent claim on a
// rotating address is a name lost for ever.
func TestNameHoldExpires(t *testing.T) {
	srv, _, clk := ownerSrv(t, "")
	if rec := do(t, srv, postReq(t, "/api/chat/dev/bedford", "alice", "here")); rec.Code != 200 {
		t.Fatalf("first: %d %s", rec.Code, rec.Body)
	}
	*clk = clk.Add(25 * time.Hour) // past NameHold
	r := postReq(t, "/api/chat/dev/bedford", "alice", "the name came free")
	r.RemoteAddr = "198.51.100.7:1234"
	if rec := do(t, srv, r); rec.Code != http.StatusOK {
		t.Fatalf("after the hold lapsed: got %d %s, want 200", rec.Code, rec.Body)
	}
}

// NameHold == 0 disables the check, and that has to keep working: it is the
// configuration every existing test runs under.
func TestNameHoldZeroDisablesTheCheck(t *testing.T) {
	srv, _, _ := newServer(t)
	if rec := do(t, srv, postReq(t, "/api/chat/dev/bedford", "alice", "one")); rec.Code != 200 {
		t.Fatalf("first: %d %s", rec.Code, rec.Body)
	}
	r := postReq(t, "/api/chat/dev/bedford", "alice", "two")
	r.RemoteAddr = "198.51.100.7:1234"
	if rec := do(t, srv, r); rec.Code != http.StatusOK {
		t.Fatalf("with NameHold unset the name is not held: got %d", rec.Code)
	}
}

// THE DEFAULT NAME MUST STAY FREE. "anon" is DefaultMoniker — who you are when
// you have not said who you are — so reserving it refuses every post from
// anyone who never typed a name. It read like the most obviously reserved word
// on the list and was the one that broke eight tests.
func TestDefaultMonikerIsNotReserved(t *testing.T) {
	if IsReservedName(DefaultMoniker) {
		t.Fatalf("%q is the default display name; reserving it silences every anonymous poster", DefaultMoniker)
	}
	srv, _, _ := ownerSrv(t, "s3cret")
	if rec := do(t, srv, postReq(t, "/api/chat/dev/bedford", DefaultMoniker, "hello")); rec.Code != http.StatusOK {
		t.Fatalf("posting as %q: got %d %s, want 200", DefaultMoniker, rec.Code, rec.Body)
	}
}

// And the hold must not fence it off either: two strangers both posting as
// "anon" is the normal case, not an impersonation.
func TestDefaultMonikerIsNotHeld(t *testing.T) {
	srv, _, _ := ownerSrv(t, "")
	if rec := do(t, srv, postReq(t, "/api/chat/dev/bedford", DefaultMoniker, "one")); rec.Code != 200 {
		t.Fatalf("first anon: %d %s", rec.Code, rec.Body)
	}
	r := postReq(t, "/api/chat/dev/bedford", DefaultMoniker, "two")
	r.RemoteAddr = "198.51.100.7:1234"
	if rec := do(t, srv, r); rec.Code != http.StatusOK {
		t.Fatalf("a second anon: got %d %s, want 200", rec.Code, rec.Body)
	}
}

// THE ASK: "reserve jae777 and jaekwon777 … or jae\d\d\d etc. i just want 'jae'
// to be me." Standing NEXT to a name is the same trick as wearing it.
//
// TWO ROUTES, BOTH NEEDED. nameSkeleton folds digits into the letters they
// imitate (7→t, 1→l, 4→a), which catches "j4e" and turns "jae777" into
// "jaettt" — a different word. letterCore strips instead of folding and catches
// the suffix. Neither alone covers both.
func TestOwnerNamesCoverDecoratedForms(t *testing.T) {
	held := []string{
		"jae777", "jaekwon777", "jae1", "jae123", "JAE777",
		"jae_777", "jae-777", "jae 777", "jae.777",
		"j4e",       // leet, caught by the skeleton fold
		"jaekwon_1", // suffix on the long form
	}
	for _, n := range held {
		if !IsOwnerName(n) {
			t.Errorf("%q should be held for the operator", n)
		}
	}
	srv, _, _ := ownerSrv(t, "s3cret")
	for _, n := range []string{"jae777", "jaekwon777", "j4e"} {
		if rec := do(t, srv, postReq(t, "/api/chat/dev/bedford", n, "hi")); rec.Code != http.StatusConflict {
			t.Errorf("posting as %q: got %d, want 409", n, rec.Code)
		}
	}
}

// AND IT MUST NOT SWALLOW STRANGERS. The rule is "this reads AS Jae", not "this
// contains jae" — a person called Jaeger keeps their name.
func TestOwnerNamesDoNotSwallowLongerNames(t *testing.T) {
	for _, n := range []string{"jaeger", "jaeden", "jaewon", "jaya", "ajae", "jaekwonx", "kwon"} {
		if IsOwnerName(n) {
			t.Errorf("%q is somebody else's name and must stay free", n)
		}
	}
	srv, _, _ := ownerSrv(t, "s3cret")
	if rec := do(t, srv, postReq(t, "/api/chat/dev/bedford", "jaeger", "hi")); rec.Code != http.StatusOK {
		t.Fatalf("jaeger: got %d %s, want 200", rec.Code, rec.Body)
	}
}

// The same decoration rule applies to roles: "admin1" borrows what "admin" does.
func TestReservedNamesCoverDecoratedForms(t *testing.T) {
	for _, n := range []string{"admin1", "admin123", "clerk99", "mod_1", "system-2"} {
		if !IsReservedName(n) {
			t.Errorf("%q should be reserved", n)
		}
	}
	// …without swallowing ordinary words that merely start the same way.
	for _, n := range []string{"adminium", "moderna", "systemic", "clerkson", "rooted"} {
		if IsReservedName(n) {
			t.Errorf("%q is an ordinary name and must stay free", n)
		}
	}
}

// CLAIM ONCE, THEN JUST TYPE. "i don't want to do it every post."
func TestOwnerNameIsClaimedOnceThenHeld(t *testing.T) {
	srv, _, clk := ownerSrv(t, "s3cret")
	// 1. The claim: token present, nobody holds it yet.
	r := postReq(t, "/api/chat/dev/bedford", "jae", "claiming")
	r.Header.Set("X-Kourt-Owner", "s3cret")
	if rec := do(t, srv, r); rec.Code != http.StatusOK {
		t.Fatalf("claim: got %d %s", rec.Code, rec.Body)
	}
	// 2. Afterwards, no token, same address — and in a DIFFERENT court, because
	//    the claim is the person and not the room.
	*clk = clk.Add(time.Minute)
	if rec := do(t, srv, postReq(t, "/api/chat/dev/covid", "jae", "no token now")); rec.Code != http.StatusOK {
		t.Fatalf("post after claiming, other court: got %d %s, want 200", rec.Code, rec.Body)
	}
	// 3. A stranger still cannot, token or no token.
	*clk = clk.Add(time.Minute)
	r3 := postReq(t, "/api/chat/dev/bedford", "jae", "impostor")
	r3.RemoteAddr = "198.51.100.9:1234"
	if rec := do(t, srv, r3); rec.Code != http.StatusConflict {
		t.Fatalf("a stranger after the claim: got %d, want 409", rec.Code)
	}
}

// The hold lapses with the address, and the token is how the name comes back —
// including back FROM an address that took it while it was free.
func TestOwnerNameCanBeReclaimedWithTheToken(t *testing.T) {
	srv, _, clk := ownerSrv(t, "s3cret")
	r := postReq(t, "/api/chat/dev/bedford", "jae", "claiming")
	r.Header.Set("X-Kourt-Owner", "s3cret")
	if rec := do(t, srv, r); rec.Code != http.StatusOK {
		t.Fatalf("claim: %d %s", rec.Code, rec.Body)
	}
	*clk = clk.Add(25 * time.Hour) // the hold lapses
	// A new address (the operator's, after an IP change) re-claims with the token.
	r2 := postReq(t, "/api/chat/dev/bedford", "jae", "re-claiming from a new address")
	r2.RemoteAddr = "203.0.113.5:1234"
	r2.Header.Set("X-Kourt-Owner", "s3cret")
	if rec := do(t, srv, r2); rec.Code != http.StatusOK {
		t.Fatalf("re-claim: got %d %s, want 200", rec.Code, rec.Body)
	}
	// And the OLD address no longer holds it.
	*clk = clk.Add(time.Minute)
	if rec := do(t, srv, postReq(t, "/api/chat/dev/bedford", "jae", "stale")); rec.Code != http.StatusConflict {
		t.Fatalf("the previous address after a re-claim: got %d, want 409", rec.Code)
	}
}
