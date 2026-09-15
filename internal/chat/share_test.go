package chat

import (
	"bytes"
	"context"
	"image"
	_ "image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jaekwon/kourt/internal/archive"
)

type fakeCardFacts struct {
	card archive.ClaimCard
	err  error
}

func (f fakeCardFacts) ClaimCardOf(context.Context, string, uint64) (archive.ClaimCard, error) {
	return f.card, f.err
}

func shareSrv(t *testing.T, f interface {
	ClaimCardOf(context.Context, string, uint64) (archive.ClaimCard, error)
}) *Server {
	t.Helper()
	srv, _, _ := newServer(t)
	srv.Facts = f
	srv.ShareOrigin = "https://kourt.xyz"
	return srv
}

// THE WHOLE POINT: the claim's own title and status must be in the HTML a
// crawler receives, because a crawler runs no JavaScript and never sees the
// fragment the app routes on.
func TestSharePageCarriesTheClaimsOwnCard(t *testing.T) {
	srv := shareSrv(t, fakeCardFacts{card: archive.ClaimCard{
		Title:  "The furin cleavage site has no close analogue in the sampled sarbecoviruses.",
		Status: "open — stake YES or NO",
	}})
	rec := do(t, srv, httptest.NewRequest(http.MethodGet, "/s/covid/7", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200: %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`property="og:title" content="The furin cleavage site`,
		`property="og:description" content="open — stake YES or NO"`,
		`name="twitter:card" content="summary_large_image"`,
		// The claim's OWN card now, not the site hero — see
		// TestSharePageImagePointsAtTheClaimsOwnCard.
		`property="og:image" content="https://kourt.xyz/s/covid/7.png"`,
		`property="og:url" content="https://kourt.xyz/s/covid/7"`,
		`https://kourt.xyz/#/c/covid/7`, // the human's destination
	} {
		if !strings.Contains(body, want) {
			t.Errorf("share page is missing %q", want)
		}
	}
}

// ABSOLUTE, NOT RELATIVE. Open Graph drops a relative og:image rather than
// resolving it, which loses the picture silently — the exact failure this whole
// route exists to fix.
func TestSharePageImageIsAbsolute(t *testing.T) {
	srv := shareSrv(t, fakeCardFacts{card: archive.ClaimCard{Title: "t"}})
	rec := do(t, srv, httptest.NewRequest(http.MethodGet, "/s/covid/1", nil))
	if strings.Contains(rec.Body.String(), `content="/og.png"`) {
		t.Fatal("og:image is relative; crawlers drop it")
	}
}

// A title is attacker-supplied text reaching markup. It must be escaped in
// every tag it lands in, and in the script that redirects.
func TestSharePageEscapesTheTitle(t *testing.T) {
	srv := shareSrv(t, fakeCardFacts{card: archive.ClaimCard{
		Title: `"><script>alert(1)</script>`, Status: `"onmouseover="x`,
	}})
	rec := do(t, srv, httptest.NewRequest(http.MethodGet, "/s/covid/1", nil))
	body := rec.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatal("the title escaped its attribute")
	}
	if strings.Contains(body, `onmouseover="x`) {
		t.Fatal("the status escaped its attribute")
	}
}

// A slow or broken node costs the PREVIEW and nothing else: the app renders the
// claim perfectly well from the browser, so the reader is sent on.
func TestSharePageRedirectsWhenTheChainFails(t *testing.T) {
	srv := shareSrv(t, fakeCardFacts{err: context.DeadlineExceeded})
	rec := do(t, srv, httptest.NewRequest(http.MethodGet, "/s/covid/3", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("got %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasSuffix(loc, "/#/c/covid/3") {
		t.Fatalf("redirect went to %q", loc)
	}
}

// THE PROPERTY, NOT THE STATUS CODE: a malformed path must never render a card.
//
// Asserting 404 was wrong and hid what actually happens. http.ServeMux CLEANS a
// path and answers 301 before any handler runs, so "/s/../etc/passwd/1" becomes
// "/s/1"… no: it becomes "/etc/passwd/1", a path this mux does not serve, and
// "/s/covid/../../admin" becomes "/admin". The traversal is neutralised rather
// than followed — which is the thing worth pinning. A test that demanded 404
// would have failed on correct, safe behaviour and invited someone to "fix" the
// redirect away.
func TestSharePageRendersNoCardForMalformedPaths(t *testing.T) {
	srv := shareSrv(t, fakeCardFacts{card: archive.ClaimCard{Title: "SECRET-CARD-TITLE"}})
	for _, p := range []string{
		"/s/covid", "/s/covid/0", "/s/covid/x", "/s//1", "/s/covid/1/2",
		"/s/COVID/1",                    // the app's own courts are lower-case
		"/s/../etc/passwd/1",            // traversal, cleaned out of /s/ entirely
		"/s/covid/../../admin",          // ditto
		"/s/covid/99999999999999999999", // overflows uint64
	} {
		rec := do(t, srv, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code == http.StatusOK {
			t.Errorf("%s: answered 200; a malformed path must not render a page", p)
		}
		if strings.Contains(rec.Body.String(), "SECRET-CARD-TITLE") {
			t.Errorf("%s: rendered a claim card", p)
		}
		// And a cleaned path must never land back inside /s/ with a claim on it.
		if loc := rec.Header().Get("Location"); strings.HasPrefix(loc, "/s/") &&
			strings.Count(strings.Trim(loc, "/"), "/") >= 2 {
			t.Errorf("%s: redirected to a share path %q", p, loc)
		}
	}
}

// ---- the rendered card ----------------------------------------------------

func TestClaimCardRendersAPNG(t *testing.T) {
	b, err := RenderClaimCard(archive.ClaimCard{
		Title: "A claim", Status: "open", Yes: 3, No: 1}, "covid")
	if err != nil {
		t.Fatal(err)
	}
	im, format, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not a decodable image: %v", err)
	}
	if format != "png" {
		t.Fatalf("format %q, want png", format)
	}
	// 1200x630 is what the crawlers crop to; a different size is silently
	// letterboxed or cropped by them, which is how a card goes subtly wrong.
	if got := im.Bounds().Dx(); got != 1200 {
		t.Errorf("width %d, want 1200", got)
	}
	if got := im.Bounds().Dy(); got != 630 {
		t.Errorf("height %d, want 630", got)
	}
}

// A claim nobody has staked on must not show an even bar: a 50/50 split implies
// a disagreement that has not happened, which is the one thing a court card
// must not invent. Compared against a rendering WITH stake so the assertion
// fails if the bar stops being drawn at all.
func TestClaimCardDrawsNoBarWithoutStake(t *testing.T) {
	bare, err := RenderClaimCard(archive.ClaimCard{Title: "A claim", Status: "open"}, "covid")
	if err != nil {
		t.Fatal(err)
	}
	staked, err := RenderClaimCard(archive.ClaimCard{
		Title: "A claim", Status: "open", Yes: 3, No: 1}, "covid")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(bare, staked) {
		t.Fatal("the bar is not being drawn at all; both renderings are identical")
	}
}

// The wordmark and the court name overlapped when the offset was a guessed
// constant rather than a measurement. Pinned by rendering a court name long
// enough that an unmeasured offset would collide.
func TestClaimCardSeparatesWordmarkFromCourt(t *testing.T) {
	a, err := RenderClaimCard(archive.ClaimCard{Title: "x"}, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderClaimCard(archive.ClaimCard{Title: "x"}, "covid")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("the court name is not drawn")
	}
}

// A title longer than the card must shrink rather than vanish, and must still
// produce a valid image.
func TestClaimCardHandlesAVeryLongTitle(t *testing.T) {
	long := strings.Repeat("a very long claim about something contested ", 12)
	b, err := RenderClaimCard(archive.ClaimCard{Title: long, Status: "open"}, "covid")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := image.Decode(bytes.NewReader(b)); err != nil {
		t.Fatalf("long title produced an undecodable image: %v", err)
	}
}

// The share page must point at the claim's own picture, not the site hero —
// the whole reported symptom was "just the default kourt hero".
func TestSharePageImagePointsAtTheClaimsOwnCard(t *testing.T) {
	srv := shareSrv(t, fakeCardFacts{card: archive.ClaimCard{Title: "t", Status: "open"}})
	rec := do(t, srv, httptest.NewRequest(http.MethodGet, "/s/covid/7", nil))
	body := rec.Body.String()
	if !strings.Contains(body, `content="https://kourt.xyz/s/covid/7.png"`) {
		t.Error("og:image does not point at the per-claim card")
	}
	if strings.Contains(body, "/og.png") {
		t.Error("og:image still points at the site hero")
	}
}

func TestShareImageServesPNG(t *testing.T) {
	srv := shareSrv(t, fakeCardFacts{card: archive.ClaimCard{Title: "t", Status: "open", Yes: 1, No: 1}})
	rec := do(t, srv, httptest.NewRequest(http.MethodGet, "/s/covid/7.png", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type %q", ct)
	}
}

// A CRAWLER THAT GETS A 500 SHOWS NO PICTURE, which is worse than the generic
// one. A failure must fall back to the hero, not error.
func TestShareImageFallsBackToTheHero(t *testing.T) {
	srv := shareSrv(t, fakeCardFacts{err: context.DeadlineExceeded})
	rec := do(t, srv, httptest.NewRequest(http.MethodGet, "/s/covid/7.png", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("got %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasSuffix(loc, "/og.png") {
		t.Fatalf("fell back to %q, want the hero", loc)
	}
}
