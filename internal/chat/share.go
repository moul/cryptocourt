package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jaekwon/kourt/internal/archive"
)

/*
The share page: /s/<court>/<id>

WHY A SERVER PATH EXISTS AT ALL, when the app already draws a share image.

The image was the wrong half of the problem. Twitter's compose intent takes
`text` and `url` and NOTHING ELSE — there is no parameter that attaches a
picture — so a downloaded PNG can only be added by hand, which is the friction
being removed. The picture in a tweet comes from the LINK's Open Graph card.

And the app's own routes cannot carry one. They are fragments (`#/c/covid/1`),
and a fragment is never sent to a server: a crawler fetching that URL receives
index.html and reads the HOME page's tags, so every claim ever shared from this
site previewed as "Kourt — Let Truth be told" with the generic map image. The
claim's own title never appeared anywhere.

So: one real path per claim, whose HTML carries that claim's card, and which
sends a human straight on to the app route. The crawler reads the tags; the
reader never notices they were here.

WHAT IT DELIBERATELY IS NOT: a second renderer of the court. It answers a title,
a status and a link. Anything more would be a copy of the app that can disagree
with it.
*/

// shareClaim serves the per-claim Open Graph page.
func (s *Server) shareClaim(w http.ResponseWriter, r *http.Request, chain, court string, id uint64) {
	if s.Facts == nil {
		// No chain client configured: send the reader on rather than showing an
		// error page for something only a crawler cares about.
		http.Redirect(w, r, s.appURL(court, id), http.StatusFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	card, err := s.Facts.ClaimCardOf(ctx, court, id)
	if err != nil {
		/* A READER IS NOT SHOWN A STACK TRACE FOR A SLOW NODE. The app route can
		   render this claim perfectly well from the browser, so a failure here
		   costs the preview and nothing else — redirect and let the app try. */
		if s.Log != nil {
			s.Log.Printf("share: %s/%s/%d: %v", chain, court, id, err)
		}
		http.Redirect(w, r, s.appURL(court, id), http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Short, not immutable: a claim's status changes when it is answered, and a
	// day-old card saying "open" under an answered claim is a lie a cache told.
	w.Header().Set("Cache-Control", "public, max-age=300")
	fmt.Fprint(w, sharePage(card, s.appURL(court, id), s.shareOrigin(), court, id))
}

// appURL is where a human belongs: the app's own route for this claim.
func (s *Server) appURL(court string, id uint64) string {
	return s.shareOrigin() + "/#/c/" + url.PathEscape(court) + "/" + strconv.FormatUint(id, 10)
}

// shareOrigin is the public origin to build absolute URLs from. Open Graph
// requires absolute image and page URLs — a relative og:image is simply dropped,
// which is a silent way to lose the picture this whole file exists to deliver.
func (s *Server) shareOrigin() string {
	if s.ShareOrigin != "" {
		return strings.TrimRight(s.ShareOrigin, "/")
	}
	return "https://kourt.xyz"
}

// sharePage is the whole document. Hand-built rather than templated: it is
// twenty lines, every one of which is a tag a crawler reads, and a template
// would put the escaping one indirection away from the values being escaped.
func sharePage(card archive.ClaimCard, appURL, origin, court string, id uint64) string {
	title := collapse(card.Title, 110)
	desc := card.Status
	if desc == "" {
		desc = "A claim of fact in the " + court + " court. Stake decides which claims get answered."
	}
	desc = collapse(desc, 200)
	e := html.EscapeString
	return `<!doctype html><html lang="en"><head><meta charset="utf-8">
<title>` + e(title) + ` — Kourt</title>
<link rel="canonical" href="` + e(appURL) + `">
<meta property="og:type" content="article">
<meta property="og:site_name" content="Kourt">
<meta property="og:url" content="` + e(origin) + `/s/` + e(court) + `/` + strconv.FormatUint(id, 10) + `">
<meta property="og:title" content="` + e(title) + `">
<meta property="og:description" content="` + e(desc) + `">
<meta property="og:image" content="` + e(origin) + `/s/` + e(court) + `/` + strconv.FormatUint(id, 10) + `.png">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta property="og:image:alt" content="The claim, and how its stake splits between YES and NO.">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="` + e(title) + `">
<meta name="twitter:description" content="` + e(desc) + `">
<meta name="twitter:image" content="` + e(origin) + `/s/` + e(court) + `/` + strconv.FormatUint(id, 10) + `.png">
<meta http-equiv="refresh" content="0; url=` + e(appURL) + `">
</head><body>
<p><a href="` + e(appURL) + `">` + e(title) + `</a></p>
<script>location.replace(` + jsString(appURL) + `)</script>
</body></html>`
}

// collapse folds whitespace and truncates on a word boundary. A card is read at
// a glance; a title cut mid-word reads as broken rather than as abridged.
func collapse(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	if i := strings.LastIndexByte(cut, ' '); i > max/2 {
		cut = cut[:i]
	}
	return cut + "…"
}

// jsString is a JSON-encoded string literal, which is a safe JS literal too —
// the redirect below is the one place a value reaches script rather than markup,
// and html.EscapeString is the wrong tool inside a <script>.
func jsString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// share parses /s/<court>/<id> and hands off. Anything else is a 404 rather
// than a guess: a malformed share link should fail visibly at the sharer, not
// quietly render somebody else's claim.
func (s *Server) share(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/s/"), "/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	court, idStr := parts[0], parts[1]
	/* THE IMAGE SHARES THIS ROUTE, distinguished only by the extension, so a
	   card and its picture can never drift to different claims — one parser,
	   one pair of values. */
	wantPNG := strings.HasSuffix(idStr, ".png")
	idStr = strings.TrimSuffix(idStr, ".png")
	// The same shape the app's own routes accept. A court name is a path segment
	// chosen by whoever made the court, so it is matched, not trusted.
	if !courtNameRe.MatchString(court) {
		http.NotFound(w, r)
		return
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		http.NotFound(w, r)
		return
	}
	if wantPNG {
		s.shareImage(w, r, court, id)
		return
	}
	s.shareClaim(w, r, "", court, id)
}

var courtNameRe = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// shareImage draws the claim's own card.
//
// FALLS BACK TO THE HERO rather than to an error. A crawler that asks for a
// picture and gets a 500 shows NO picture at all, which is strictly worse than
// the generic one — so a slow node or a render failure redirects to /og.png and
// the post still carries an image.
func (s *Server) shareImage(w http.ResponseWriter, r *http.Request, court string, id uint64) {
	hero := func() { http.Redirect(w, r, s.shareOrigin()+"/og.png", http.StatusFound) }
	if s.Facts == nil {
		hero()
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	card, err := s.Facts.ClaimCardOf(ctx, court, id)
	if err != nil {
		if s.Log != nil {
			s.Log.Printf("share image: %s/%d: %v", court, id, err)
		}
		hero()
		return
	}
	png, err := RenderClaimCard(card, court)
	if err != nil {
		if s.Log != nil {
			s.Log.Printf("share image: render %s/%d: %v", court, id, err)
		}
		hero()
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// Five minutes, matching the page: the bar moves when somebody stakes, and a
	// day-old picture of a split that has changed is a lie a cache told.
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Header().Set("Content-Length", strconv.Itoa(len(png)))
	if r.Method == http.MethodHead {
		return
	}
	w.Write(png)
}
