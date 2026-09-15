package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/jaekwon/kourt/internal/gnorpc"
)

// The chain is the only thing that can say a blob is worth keeping.
//
// PROMOTION IS THE WHOLE ANTI-ABUSE STORY. Staged bytes expire in an hour; they
// become permanent only when a claim on chain is seen to reference their hash.
// That reference is the one thing an attacker cannot fabricate or get for free,
// because filing a claim costs a deposit the court already charges.
//
// So this file is deliberately the smallest chain client that can answer one
// question — "does claim N of court C reference this hash?" — and nothing else.
//
// THE ENVELOPE MOVED OUT, the questions stayed. The JSON-RPC framing that used to
// live here is now internal/gnorpc, because the Discord bridge needed the same
// envelope for an unrelated question and the envelope is the part that is easy to
// get wrong: the payload nests under ResponseBase on some nodes, it is base64,
// and a query error arrives in a different field from a transport error — each of
// which yields an empty string rather than a failure when mishandled. The
// argument above still holds for everything below it: these are archive's
// questions and belong to archive.

// Chain reads claim media from a gno node over JSON-RPC.
type Chain struct {
	// RPC is the node endpoint, e.g. https://rpc.kourt.xyz.
	RPC string
	// PkgPath is the realm, e.g. gno.land/r/kourt/kourtv2.
	PkgPath string
	// HTTP is the client used for queries; nil means a 10-second default.
	HTTP *http.Client
}

func (c *Chain) qeval(ctx context.Context, expr string) (string, error) {
	n := &gnorpc.Node{RPC: c.RPC, HTTP: c.HTTP, ID: "archive"}
	return n.QEval(ctx, c.PkgPath, expr)
}

// ClaimCount is how many claims a court has ever opened, so backfill knows
// where the end is.
func (c *Chain) ClaimCount(ctx context.Context, court string) (uint64, error) {
	out, err := c.qeval(ctx, fmt.Sprintf("ClaimCount(%q)", court))
	if err != nil {
		return 0, err
	}
	// qeval answers `(12 uint64)`.
	var n uint64
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "(%d", &n); err != nil {
		return 0, fmt.Errorf("claim count was not a number: %q", out)
	}
	return n, nil
}

// mediaItem is the shape ClaimMedia publishes. Only the hash is read here — the
// archive has no opinion about captions or dimensions.
type mediaItem struct {
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
	Purged bool   `json:"purged"`
}

// ClaimHashes returns the sha256s a claim references, as the chain reports them.
//
// A purged item yields nothing: the court has withdrawn its pointer to those
// bytes, so nothing here should be buying them permanent storage.
func (c *Chain) ClaimHashes(ctx context.Context, court string, claimID uint64) ([]string, error) {
	out, err := c.qeval(ctx, fmt.Sprintf("ClaimMedia(%q,%d)", court, claimID))
	if err != nil {
		return nil, err
	}
	return mediaHashes(out, "claim media")
}

// FolderHashes returns the sha256s a FOLDER references — its one picture.
//
// A FOLDER IS A REFERENCE TOO, and until this existed it was not one. The realm
// has always had SetFolderImage and the overlay has always drawn `.mfimg` from
// it, but promotion ran over claims alone: `GetServable` serves `promoted = 1`
// only, so a folder's picture was uploaded, staged, never promoted, swept an
// hour later, and until then served as a 404. Every part worked and the picture
// could not appear on any deployment. Found on kourt.xyz the first time a folder
// was given one.
//
// FolderImage answers the same JSON as ClaimMedia — one item rather than a list,
// but the same shape — so this reads it the same way, purge rule included.
func (c *Chain) FolderHashes(ctx context.Context, court string, folderID uint64) ([]string, error) {
	out, err := c.qeval(ctx, fmt.Sprintf("FolderImage(%q,%d)", court, folderID))
	if err != nil {
		return nil, err
	}
	return mediaHashes(out, "folder image")
}

// ImagedFolders lists the folders of a court that carry a picture, in one read.
//
// THE TREE ALREADY KNOWS. FolderTree answers "id:parent:flags:bornOf" for every
// folder in the court, and `i` in the flags means this folder has an image — so
// the folders worth asking about are named by a read the client already makes,
// and a hundred FolderImage queries per pass become one plus the few that say
// yes. A court with no pictures costs exactly one query.
//
// A FOLDER'S PICTURE CAN BE SET AT ANY TIME, which is why backfill cannot walk
// folders behind a cursor the way it walks claims. A claim's evidence is fixed
// when it is filed, so a forward-only cursor sees all of it; a moderator can
// give folder 2 a picture years after folder 900 was made, and a cursor past it
// would never look again. This lists them all, every pass, cheaply.
func (c *Chain) ImagedFolders(ctx context.Context, court string) ([]uint64, error) {
	out, err := c.qeval(ctx, fmt.Sprintf("FolderTree(%q)", court))
	if err != nil {
		return nil, err
	}
	return imagedFolders(out), nil
}

// imagedFolders parses FolderTree's rows. Split out so the format is tested
// without a node, since this is the one place the archive reads that wire shape.
func imagedFolders(out string) []uint64 {
	body := unquoteQeval(out)
	var ids []uint64
	for _, row := range strings.Split(body, ",") {
		f := strings.Split(strings.TrimSpace(row), ":")
		if len(f) < 3 || !strings.Contains(f[2], "i") {
			continue
		}
		// A purged or retired folder still has its bytes referenced by the chain
		// until the image itself is cleared, and FolderImage is what says so —
		// this only decides who to ASK.
		id, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil || id == 0 {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// unquoteQeval strips the `("<json>" string)` wrapper qeval puts around a string
// answer. One spelling, because two readers of the same wire format is how they
// come to disagree about an escape.
func unquoteQeval(out string) string {
	body := out
	if i, j := bytes.IndexByte([]byte(body), '"'), bytes.LastIndexByte([]byte(body), '"'); i >= 0 && j > i {
		var unquoted string
		if err := json.Unmarshal([]byte(body[i:j+1]), &unquoted); err == nil {
			body = unquoted
		}
	}
	return body
}

// mediaHashes reads the JSON ClaimMedia and FolderImage both publish.
//
// A purged item yields nothing: the court has withdrawn its pointer to those
// bytes, so nothing here should be buying them permanent storage.
func mediaHashes(out, what string) ([]string, error) {
	var items []mediaItem
	if err := json.Unmarshal([]byte(unquoteQeval(out)), &items); err != nil {
		return nil, fmt.Errorf("%s was not the expected JSON: %w", what, err)
	}
	hashes := make([]string, 0, len(items))
	for _, it := range items {
		if it.Purged || !digestRe.MatchString(it.SHA256) {
			continue
		}
		hashes = append(hashes, it.SHA256)
	}
	return hashes, nil
}

// ClaimCard is the handful of facts a share page needs about one claim.
//
// READ SERVER-SIDE, and that is the whole reason this exists. A social crawler
// does not run JavaScript and never sees a URL fragment, so the app's own
// `#/c/covid/1` route can tell it nothing: every claim shared from this site
// rendered the HOME page's card. The card has to be in the HTML of a real path,
// which means something on the server has to know the claim's title.
type ClaimCard struct {
	Title  string
	Status string
	Closed bool
	// Yes and No are the two stake pools, in the court's smallest unit. Both
	// zero means nothing is staked yet, which is a real state and not a failure:
	// the card then shows no bar rather than a 50/50 one, because an even split
	// implies a disagreement nobody has actually had.
	Yes, No int64
}

// ClaimCardOf reads one claim's shareable facts.
//
// TITLE FIRST AND ALONE-SUFFICIENT: a missing status is a worse card, a missing
// title is not a card at all, so the status is fetched but its failure is not
// fatal. An empty title means no such claim, which the caller turns into a 404
// rather than a page about nothing.
func (c *Chain) ClaimCardOf(ctx context.Context, court string, claimID uint64) (ClaimCard, error) {
	out, err := c.qeval(ctx, fmt.Sprintf("ClaimTitle(%q,%d)", court, claimID))
	if err != nil {
		return ClaimCard{}, err
	}
	card := ClaimCard{Title: unescapeMarkdown(unquoteQeval(out))}
	if card.Title == "" {
		return ClaimCard{}, fmt.Errorf("no claim %d in %q", claimID, court)
	}
	if out, err := c.qeval(ctx, fmt.Sprintf("ClaimStatus(%q,%d)", court, claimID)); err == nil {
		card.Status = unescapeMarkdown(unquoteQeval(out))
	}
	// The split, for the bar. Failure is not fatal for the same reason the status
	// is not: a card without a bar is worse, a card without a title is nothing.
	// qeval answers a two-value return as `(123 int64)(456 int64)`.
	if out, err := c.qeval(ctx, fmt.Sprintf("StakePools(%q,%d)", court, claimID)); err == nil {
		nums := regexp.MustCompile(`\(\s*(-?\d+)\s+int64\s*\)`).FindAllStringSubmatch(out, -1)
		if len(nums) == 2 {
			card.Yes, _ = strconv.ParseInt(nums[0][1], 10, 64)
			card.No, _ = strconv.ParseInt(nums[1][1], 10, 64)
		}
	}
	return card, nil
}

// unescapeMarkdown undoes the render-side escaping the realm stores titles with.
//
// A claim filed as "…at the Wuhan Institute of Virology." is STORED as
// "…Virology\." — the realm escapes punctuation so its own markdown Render does
// not reinterpret it. The app's card calls unesc() before drawing; this path did
// not, so the share image and the og:title both published a stray backslash.
// Visible on covid/10 as "Virology\.".
//
// ONLY BEFORE PUNCTUATION, never a blanket backslash strip: a backslash is a
// legitimate character in a claim, and "C:\Users" must survive. The escape the
// realm writes is always a backslash immediately before an ASCII punctuation
// mark, which is exactly what this undoes and nothing else.
func unescapeMarkdown(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && isASCIIPunct(s[i+1]) {
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isASCIIPunct(c byte) bool {
	return (c >= '!' && c <= '/') || (c >= ':' && c <= '@') ||
		(c >= '[' && c <= '`') || (c >= '{' && c <= '~')
}
