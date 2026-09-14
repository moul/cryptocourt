#!/usr/bin/env python3
"""Build a concept file: the real fragment + the real CSS + this concept's overrides.

Every concept is judged against the SAME content — the captured claim page, with
its real numbers, real chart and real ticket — so the only variable is the CSS.
A concept that looks good over lorem ipsum has proved nothing.
"""
import io, sys, pathlib
HERE = pathlib.Path(__file__).parent
def build(slug, title, blurb, css, view="claim"):
    """view: "claim" (the default fragment), "docket", "directory" or "embed".

    Some ideas can only be judged on a table — tabular figures need a stacked
    column of multi-digit numbers, and the stats strip is a court-page element.
    Against the claim page those rules measured `statsCells: 0`, i.e. untested.

    "embed" is the share card, and it needs the `embed` class on <html> because
    that is what the real page keys the whole treatment off (rail hidden, banner
    hidden, card filling the frame). Judge it at 400x500 — the size the embed
    snippet actually asks for — not at desktop width.
    """
    names = {"docket": "_fragment-docket.html", "directory": "_fragment-directory.html",
             "embed": "_fragment-embed.html"}
    frag = (HERE/names.get(view, "_fragment.html")).read_text(encoding="utf-8")
    base = (HERE/"_base.css").read_text(encoding="utf-8")
    root = ' class="embed"' if view == "embed" else ""
    out = f"""<!doctype html><html lang="en" data-theme="dark"{root}><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{title} — Kourt concept</title>
<style>{base}</style>
<style>
/* ===================== CONCEPT: {title} =====================
{blurb}
   ===================================================================== */
{css}
</style></head><body>
{frag}
</body></html>"""
    (HERE/f"{slug}.html").write_text(out, encoding="utf-8")
    print(f"  wrote {slug}.html")
