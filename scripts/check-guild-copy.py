#!/usr/bin/env python3
"""What a Discord listing means is said in two places, and both have to say it.

  internal/binding/server.go   sends `disclosure` with every listing it serves
  web/index.html               prints the sentence under the button a reader clicks
  internal/discordapi          pins it INSIDE the server, for the reader who
                               arrived through a shared invite and will never
                               open the court page

They exist for different reasons and neither can be deleted for the other. The
service's copy is what any OTHER client gets — a bot, a mirror, somebody reading
the API — and the overlay's copy is what the person actually about to click sees.

WHAT DRIFT LOOKS LIKE, AND WHY IT IS SILENT. The failure is not the two disagreeing
about wording; it is the overlay keeping the button and losing the sentence. A
listing certifies that a court's CURRENT MODERATORS chose a server and nothing
whatever about the court or about what is said inside it, and a reader who takes
"listed on kourt.xyz" for a warrant has been misled by us rather than by whoever
runs the server. Nothing else in the tree fails when that paragraph is trimmed for
space: the page renders, the tests pass, the link works.

THE SERVICE IS THE AUTHORITY AND THE OVERLAY MAY ADD. This checks containment, not
equality — the overlay says one sentence more ("It is run by them, not by the
court"), which is a strengthening. What it may not do is say less.

TWO DEFINITIONS RATHER THAN A SHARED CONSTANT, for the reason check-addr-shapes.py
already gives: the overlay is one self-contained static file with no build step
and no way to import a Go string. A guard is cheaper than a bundler.

WHAT THIS DOES NOT CHECK. Not that the sentence is TRUE, and not that it is
prominent — a disclosure in four-point grey passes this and fails a reader.
web/tests/browser/discord_panel.js measures that it reaches the painted page;
this only holds the two spellings together.
"""
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
GO = os.path.join(ROOT, "internal", "binding", "server.go")
JS = os.path.join(ROOT, "web", "index.html")
DISCORD = os.path.join(ROOT, "internal", "discordapi", "client.go")


def norm(s):
    """Whitespace-insensitive: one is wrapped for Go, the other for HTML."""
    return re.sub(r"\s+", " ", s).strip()


def deixis(s):
    """Fold "said here" and "said there" together.

    THE ONE DIFFERENCE THAT IS NOT DRIFT. The clause makes the same claim in all
    three places, but the speaker is standing somewhere different each time: the
    court page says "anything said THERE is true" about a server the reader has
    not opened, and the notice pinned inside that server says "anything said HERE".
    Forcing them to match word for word would put one of them in the wrong place
    — and a disclosure that reads as though it were written for somewhere else is
    a disclosure a reader discounts.

    Only this pair is folded, and only in this clause. Everything else still has
    to match exactly, which is the point of the guard.
    """
    return s.replace("anything said here", "anything said there")


def go_disclosure():
    src = open(GO).read()
    # A Go string built by concatenating adjacent quoted parts across lines.
    m = re.search(r'"disclosure":\s*((?:"[^"]*"\s*\+?\s*)+)', src)
    if not m:
        sys.exit(
            "check-guild-copy: no `disclosure` string found in %s. If the listing "
            "stopped carrying one, that is the bug this guard is about — a listing "
            "without its sentence is a claim the service did not mean to make. If "
            "it merely moved, point this guard at the new home."
            % os.path.relpath(GO, ROOT)
        )
    return norm("".join(re.findall(r'"([^"]*)"', m.group(1))))


def js_disclosure():
    src = open(JS).read()
    m = re.search(r"function discordHtml\(.*?\n\}", src, re.S)
    if not m:
        sys.exit(
            "check-guild-copy: discordHtml() not found in web/index.html — that is "
            "the function that renders the listing, so either it was renamed or the "
            "overlay stopped rendering one."
        )
    return norm(m.group(0))


def in_guild_disclosure():
    src = open(DISCORD).read()
    m = re.search(r"func disclosureText\(.*?\n\}", src, re.S)
    if not m:
        sys.exit(
            "check-guild-copy: disclosureText() not found in internal/discordapi — "
            "that is the notice pinned inside a listed server, so either it was "
            "renamed or the bot stopped posting one."
        )
    # THE LITERALS, JOINED, like the service side. The notice is built by
    # concatenating quoted parts around interpolated values, so the raw function
    # text splits the clause across `" + "` and no contiguous match is possible.
    # Joining the quoted parts closes those seams; the interpolated values become
    # gaps, which is right — a court's name is not part of the sentence being
    # held steady. Escaped newlines become spaces so norm() can do the rest.
    parts = re.findall(r'"((?:[^"\\]|\\.)*)"', m.group(0))
    return norm("".join(parts).replace("\\n", " "))


def main():
    go = deixis(go_disclosure())
    js = deixis(js_disclosure())
    guild = deixis(in_guild_disclosure())

    # VACUITY FIRST. An empty or trivial extraction would satisfy the containment
    # test below perfectly while checking nothing, and that is the shape
    # check-guards-blind.py exists to catch. Checked before the comparison, so a
    # single edit that broke both cannot make these arms unreachable.
    if len(go) < 60:
        sys.exit(
            "check-guild-copy: the service's disclosure extracted as %r, which is "
            "too short to be the sentence. The extraction is broken, and a broken "
            "extraction passes every comparison in this guard." % go
        )
    for must in ("moderators", "does not mean"):
        if must not in go:
            sys.exit(
                "check-guild-copy: the service's disclosure no longer contains %r. "
                "Either the sentence stopped saying who chose the server and what "
                "that does not imply, or the extraction is matching the wrong "
                "string.\n  got: %s" % (must, go)
            )

    if go not in js:
        sys.exit(
            "check-guild-copy: the overlay no longer says what the service says.\n"
            "  service: %s\n"
            "  overlay: %s\n\n"
            "The overlay may say MORE — it already does. It may not say less: this "
            "sentence is the whole difference between publishing a link and "
            "vouching for what is behind it." % (go, js[:400])
        )

    # THE THIRD COPY, and the one a reader is likeliest to meet: most people
    # arrive through a shared invite rather than through the court page.
    if go not in guild:
        sys.exit(
            "check-guild-copy: the notice pinned inside a listed server no longer "
            "says what the service says.\n"
            "  service: %s\n"
            "  in-guild: %s\n\n"
            "A reader who checks the court page and the server and finds two "
            "accounts of what a listing means has been given a reason to trust "
            "neither." % (go, guild[:400])
        )

    # The button and the sentence are one thing. A render path that can emit the
    # anchor without the paragraph is the drift this guard cannot see by
    # comparing strings, so it is asserted structurally.
    if "rel=\"noopener\"" not in js:
        sys.exit(
            "check-guild-copy: discordHtml() renders an external link without "
            "rel=\"noopener\". deploy.sh refuses to ship that, so this would fail "
            "at deploy time instead — earlier is better."
        )

    print(
        "check-guild-copy: the listing's disclosure (%d chars) is identical in all "
        "three places it is said — the service, the overlay, and the notice pinned "
        "inside a listed server." % len(go)
    )


if __name__ == "__main__":
    main()
