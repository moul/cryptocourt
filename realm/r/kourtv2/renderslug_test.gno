package kourtv2

import (
	"strings"
	"testing"

	testutils "gno.land/p/nt/testutils/v0"
)

/* EVERY ROUTE MUST SURVIVE A SLUG THAT IS NOT A COURT.

   Render(path) takes attacker-controlled input, and render() dispatches on the
   path's shape before anything knows whether the court exists. Each arm is
   therefore responsible for saying "Not found" rather than panicking, and all
   but one did: renderCourt guards with Exists, renderFolderPage parses its id,
   renderElection checks its arity, the numeric arm ParseUints. renderStandingIndex
   called mustCourt directly, so `<anything>/me` threw where `<anything>`,
   `<anything>/1` and `<anything>/folder/1` all answered politely.

   FOUND BY ASKING THE LIVE CHAIN, not by reading: eight probes against
   gnoland-1, seven graceful and one panic. It is still live there, because a
   package deploys once — this pins the fixed copy so the gap cannot reopen.

   A TABLE RATHER THAN ONE CASE, because the defect was an arm somebody missed
   while writing the others. One case pins one arm; the table pins the rule. */
func TestEveryRouteRefusesAnUnknownCourt(cur realm, t *testing.T) {
	const ghost = "nosuchcourt"
	if Exists(ghost) {
		t.Fatal("the fixture needs a slug no court uses")
	}
	for _, p := range []string{
		ghost,
		ghost + "/me",
		ghost + "/mod",
		ghost + "/election",
		ghost + "/1",
		ghost + "/folder/1",
		ghost + "/me/" + testutils.TestAddress("rs-probe").String(),
		// The board arms take parts[0] raw, same as the rest.
		ghost + "/1/board",
		ghost + "/1/board/top",
		ghost + "/1/board/hidden",
		ghost + "/1/board/7",
	} {
		got := Render(p)
		if !strings.Contains(got, "Not found") {
			t.Fatalf("%q did not say Not found: %q", p, got)
		}
	}
}
