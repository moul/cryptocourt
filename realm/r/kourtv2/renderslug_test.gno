package kourtv2

import (
	"strings"
	"testing"

	testutils "gno.land/p/nt/testutils/v0"
)

/* EVERY ROUTE MUST SURVIVE INPUT THAT NAMES NOTHING.

   Render(path) takes attacker-controlled input, and render() dispatches on the
   path's SHAPE before anything knows whether the court or the claim exists. Each
   arm is therefore responsible for saying "Not found" itself.

   FIVE ARMS WERE MISSING IT, not one: renderStanding and renderStandingIndex
   (posting.gno), and renderBoardIn, renderBoardHidden and renderBoardRow
   (board.gno). Six others already had it — renderCourt, renderModLog,
   renderElection, renderFolderPage, renderClaim, renderPositions — so this was
   not one arm overlooked but two whole features written without a rule the rest
   of render() keeps.

   A TABLE, NOT A CASE. The defect was an arm somebody missed while writing the
   others, so a single case reproduces the original mistake: one case pins one
   arm, a table pins the rule. The table earned that twice — it caught the second
   standing arm after a live probe had convinced me only one was wrong, and then
   the board arms after enumeration found three more.

   BOTH AXES, because the slug guard short-circuits the ghost paths before a
   claim id is ever read. Found on gnoland-1: `meta/999999` answered politely
   while `meta/999999/board` threw. */
func TestEveryRouteRefusesAnUnknownCourt(cur realm, t *testing.T) {
	const ghost = "nosuchcourt"
	if Exists(ghost) {
		t.Fatal("the fixture needs a slug no court uses")
	}
	probe := testutils.TestAddress("rs-probe").String()

	paths := []string{
		ghost,
		ghost + "/me",
		ghost + "/mod",
		ghost + "/election",
		ghost + "/1",
		ghost + "/folder/1",
		ghost + "/me/" + probe,
		ghost + "/1/board",
		ghost + "/1/board/top",
		ghost + "/1/board/hidden",
		ghost + "/1/board/7",
		// A real court, an id that is not a claim.
		metaSlug + "/999999",
		metaSlug + "/999999/board",
		metaSlug + "/999999/board/top",
		metaSlug + "/999999/board/hidden",
		metaSlug + "/999999/board/7",
		// Zero is not an id either. Found on gnoland-1: `meta/0/board` and
		// `meta/1/board/0` both threw while `meta/0` answered politely.
		metaSlug + "/0",
		metaSlug + "/0/board",
		metaSlug + "/0/board/top",
		metaSlug + "/0/board/7",
	}
	for _, p := range paths {
		if got := Render(p); !strings.Contains(got, "Not found") {
			t.Fatalf("%q did not say Not found: %q", p, got)
		}
	}
}
