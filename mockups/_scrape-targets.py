#!/usr/bin/env python3
"""Build _targets.json — the destination list c9-commandbar.html navigates.

Every target is scraped from the three captured fragments, so the palette is
demonstrated against real routes, real court slugs and real claim titles. A
palette answering invented destinations would be the lorem-ipsum problem in
another costume.

WHY THIS FILE EXISTS. The first pass took `textContent` off each anchor. On a
row like

    <a href="#/c/orem/f/1"><span class="n">Infrastructure</span>
      <span class="sub">3 claims</span><span class="badge">3<span>claims</span></span>
      <span class="kind">folder</span></a>

that yields "Infrastructure3 claims 3claims folder" — the site's markup carries
meaning in its STRUCTURE (name, count, badge, kind) and flattening it destroys
the meaning and mashes the words together in one move. Same class of bug as an
inline-flex row rendering "filed underMunicipal record": whitespace between
elements is not free.

So each row kind is read for the field that names it, and the count comes along
as a dim secondary rather than being glued onto the name. Titles are kept WHOLE:
truncation is the renderer's job (CSS ellipsis knows the pixel width; a
44-character slice does not, and cut mid-word it reads as data loss).
"""
import html
import io
import json
import re

BASE = "/Users/jk/gopath/src/github.com/jaekwon/cryptocourt-mod/mockups/"


def read(n):
    return io.open(BASE + n, encoding="utf-8").read()


def txt(s):
    """Tags out, entities decoded, whitespace collapsed."""
    return re.sub(r"\s+", " ", html.unescape(re.sub(r"<[^>]+>", " ", s))).strip()


def cls(frag, name):
    """First `class="… name …"` element's inner text within frag."""
    m = re.search(r'class="[^"]*\b%s\b[^"]*"[^>]*>(.*?)</span>' % name, frag, re.S)
    return txt(m.group(1)) if m else ""


out, seen = [], set()


def add(href, label, sub="", kind=""):
    if not href or href in seen or not label:
        return
    seen.add(href)
    out.append({"h": href, "t": label, "s": sub, "k": kind})


docket = read("_fragment-docket.html")
claim = read("_fragment.html")
directory = read("_fragment-directory.html")

# --- claims: the whole title, plus the folder it is filed under -------------
for m in re.finditer(r'<a class="crow"[^>]*href="(#/c/[^"]+)"(.*?)</a>', docket, re.S):
    body = m.group(2)
    t = re.search(r'class="t"[^>]*>(.*?)</span>', body, re.S)
    ms = re.findall(r'class="m"[^>]*>(.*?)</span>', body, re.S)
    # A row's `.m` spans are the folder name AND, on a claim mid-settlement, a
    # whole sentence about when it settles. Both are real; only one belongs in a
    # dim secondary next to a route, so take the short one.
    #
    # The claim NUMBER is deliberately not carried here. The route column
    # already reads "#/c/orem/1"; repeating "#1" in the label spends the width
    # that the folder name then loses to the ellipsis.
    folder = next((txt(x) for x in ms if 0 < len(txt(x)) <= 30), "")
    if t:
        add(m.group(1), txt(t.group(1)), folder, "claim")

# --- folders and courts: the name, with the row's own count line -----------
# Both are `a.crow.folderrow` / `a.crow.courtrow` and both keep their count in
# an `.m` line ("4 claims · 1 subfolder", "11 claims"), which is information the
# route does not carry — unlike a court's `.id`, which is just the slug spelled
# a second time.
for frag, pat, kind in ((docket, r'<a[^>]*href="(#/c/[^"]*/f/[^"]+)"(.*?)</a>', "folder"),
                        (directory, r'<a[^>]*courtrow[^>]*href="(#/c/[^"/]+)"(.*?)</a>', "court")):
    for m in re.finditer(pat, frag, re.S):
        body = m.group(2)
        add(m.group(1), cls(body, "t") or cls(body, "n"), cls(body, "m"), kind)

# --- pages and side-routes, from every capture -----------------------------
# Rail entries carry a decorative glyph span; the glyph is meaningless in a text
# list, so the label is the anchor's text with that span removed first.
for frag in (claim, docket, directory):
    for m in re.finditer(r"<a\b([^>]*)>(.*?)</a>", frag, re.S):
        attrs, body = m.group(1), m.group(2)
        h = re.search(r'href="(#[^"]*)"', attrs)
        # The brand mark also points at #/ and would claim that route with the
        # wordmark; the rail's own entry names it better ("Directory").
        if (not h or "crow" in attrs or "courtrow" in attrs
                or 'class="mark"' in attrs or "/f/" in h.group(1)):
            continue
        label = txt(re.sub(r'<span class="g">.*?</span>', "", body, flags=re.S))
        label = label.rstrip("→↗ ").strip()
        if not label or len(label) > 60:
            continue
        add(h.group(1), label, "", "page")

out.sort(key=lambda t: ({"court": 0, "folder": 1, "claim": 2, "page": 3}[t["k"]],
                        t["h"]))
io.open(BASE + "_targets.json", "w", encoding="utf-8").write(
    json.dumps(out, ensure_ascii=False, indent=1) + "\n")

# Inline the same list into the concept, which must open from file:// with no
# fetch. One source, two consumers, so they cannot drift.
c9 = read("c9-commandbar.html")
a = c9.index("  var T=[")
b = c9.index("\n", a)
c9 = c9[:a] + "  var T=" + json.dumps(out, ensure_ascii=False) + ";" + c9[b:]
io.open(BASE + "c9-commandbar.html", "w", encoding="utf-8").write(c9)

print("%d targets" % len(out))
for t in out:
    print("  %-24s %-8s %s%s" % (t["h"], t["k"], t["t"][:52],
                                 "   (%s)" % t["s"] if t["s"] else ""))
