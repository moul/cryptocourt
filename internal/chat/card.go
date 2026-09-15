package chat

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strconv"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"

	"github.com/jaekwon/kourt/internal/archive"
)

/*
The share card, drawn server-side.

WHY THIS EXISTS AT ALL, when the page already draws a better one. The page draws
it in a CANVAS, in the reader's browser, after JavaScript runs. A social crawler
does neither: it fetches a URL, reads the tags, and takes whatever og:image
points at. So the card in the tweet could only ever be the one static hero, and
every claim shared from this site showed the same picture — reported exactly
that way: "it doesn't show the graph, just the default kourt hero".

WHAT IT IS NOT: a port of the canvas card. That one has a sparkline, a trailing
average, conviction, a media thumbnail. Re-implementing those here would be a
second renderer of the same facts, free to disagree with the first, and the
disagreement would be invisible — nobody sees both at once. This draws the four
things that make a claim legible to somebody scrolling past: whose court, what
was claimed, how the stake splits, and where it stands.

THE TYPE IS THE GO FONTS, which ship inside golang.org/x/image under a BSD
licence, so there is no font file to vendor, license or forget to deploy. They
are a sans and the site's wordmark is a serif, so this card is deliberately its
own thing rather than a poor copy of the hero.
*/

const (
	cardW, cardH = 1200, 630
	cardPad      = 56
)

var (
	cardInk    = color.RGBA{0xf2, 0xef, 0xe8, 0xff} // paper on dark
	cardBG     = color.RGBA{0x0d, 0x0d, 0x12, 0xff}
	cardMuted  = color.RGBA{0x8b, 0x8b, 0x99, 0xff}
	cardAccent = color.RGBA{0xd8, 0xa7, 0x3a, 0xff} // the rule under the wordmark
	cardYes    = color.RGBA{0x3f, 0xb9, 0x7f, 0xff}
	cardNo     = color.RGBA{0xd4, 0x5c, 0x4a, 0xff}
	cardRail   = color.RGBA{0x24, 0x24, 0x2e, 0xff}
)

// face builds a font face at a size, or nil if the font will not parse — which
// is a build-time impossibility with an embedded font and therefore not worth
// an error return that every call site would have to ignore.
func face(ttf []byte, px float64) font.Face {
	f, err := opentype.Parse(ttf)
	if err != nil {
		return nil
	}
	fc, err := opentype.NewFace(f, &opentype.FaceOptions{Size: px, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil
	}
	return fc
}

func textW(f font.Face, s string) int { return font.MeasureString(f, s).Round() }

func drawText(dst draw.Image, f font.Face, c color.Color, x, y int, s string) {
	if f == nil {
		return
	}
	d := &font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: f,
		Dot: fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)}}
	d.DrawString(s)
}

func fillRect(dst draw.Image, x, y, w, h int, c color.Color) {
	if w <= 0 || h <= 0 {
		return
	}
	draw.Draw(dst, image.Rect(x, y, x+w, y+h), image.NewUniform(c), image.Point{}, draw.Src)
}

// wrapText breaks s into lines that fit maxW, by measurement.
//
// A WORD WIDER THAN THE LINE gets its own line rather than being dropped: a
// claim may legitimately carry a hash or a URL, and silently losing one would
// change what the claim says.
func wrapText(f font.Face, s string, maxW int) []string {
	var out []string
	line := ""
	for _, w := range strings.Fields(s) {
		t := w
		if line != "" {
			t = line + " " + w
		}
		if textW(f, t) > maxW && line != "" {
			out = append(out, line)
			line = w
			continue
		}
		line = t
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// RenderClaimCard draws the 1200x630 PNG a crawler will show.
func RenderClaimCard(card archive.ClaimCard, courtName string) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, cardW, cardH))
	draw.Draw(img, img.Bounds(), image.NewUniform(cardBG), image.Point{}, draw.Src)

	// ---- wordmark + court -------------------------------------------------
	wm := face(gobold.TTF, 34)
	const mark = "KOURT"
	drawText(img, wm, cardInk, cardPad, 84, mark)
	// MEASURED, NOT GUESSED. A hard-coded offset put the court's name ON TOP of
	// the wordmark — "KOURT" at 34px bold is wider than the 96px it was given,
	// and the two overlapped in every card. The rule under the mark is measured
	// from the same string for the same reason.
	markW := textW(wm, mark)
	fillRect(img, cardPad, 100, markW, 3, cardAccent)
	if courtName != "" {
		cf := face(goregular.TTF, 22)
		drawText(img, cf, cardMuted, cardPad+markW+18, 84, strings.ToUpper(courtName))
	}

	/* ---- title, FITTED rather than truncated -----------------------------
	   A claim is a sentence and the card is the whole pitch, so cutting it is
	   the worst outcome available: drop ", according to a preliminary count"
	   and what is left reads as flat fact. Largest size that reaches four
	   lines wins; only if even the floor will not hold it is it marked cut. */
	maxW := cardW - 2*cardPad
	var lines []string
	var tf font.Face
	for _, px := range []float64{52, 46, 42, 38, 34, 30} {
		tf = face(gobold.TTF, px)
		lines = wrapText(tf, card.Title, maxW)
		if len(lines) <= 4 {
			break
		}
	}
	if len(lines) > 4 {
		lines = lines[:4]
		lines[3] = lines[3] + "…"
	}
	y := 190
	lineH := 62
	if n := len(lines); n > 0 {
		lineH = int(float64(textW(tf, "M")) * 1.9)
		if lineH < 40 {
			lineH = 40
		}
	}
	for _, l := range lines {
		drawText(img, tf, cardInk, cardPad, y, l)
		y += lineH
	}

	/* ---- the split -------------------------------------------------------
	   NO BAR WHEN NOTHING IS STAKED, rather than an even one. A 50/50 bar over
	   a claim nobody has staked on implies a disagreement that has not
	   happened, which is the one thing a court card must not invent. */
	barY := cardH - 150
	total := card.Yes + card.No
	if total > 0 {
		lf := face(gobold.TTF, 24)
		yesW := int(float64(maxW) * float64(card.Yes) / float64(total))
		fillRect(img, cardPad, barY, maxW, 26, cardRail)
		fillRect(img, cardPad, barY, yesW, 26, cardYes)
		fillRect(img, cardPad+yesW, barY, maxW-yesW, 26, cardNo)
		pct := func(v int64) string {
			return strconv.FormatFloat(float64(v)*100/float64(total), 'f', 1, 64) + "%"
		}
		drawText(img, lf, cardYes, cardPad, barY+58, "YES "+pct(card.Yes))
		no := "NO " + pct(card.No)
		drawText(img, lf, cardNo, cardW-cardPad-textW(lf, no), barY+58, no)
	}

	// ---- status ------------------------------------------------------------
	if card.Status != "" {
		sf := face(goregular.TTF, 21)
		st := card.Status
		for textW(sf, st) > maxW && strings.Contains(st, " ") {
			st = st[:strings.LastIndex(st, " ")]
		}
		drawText(img, sf, cardMuted, cardPad, cardH-42, st)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
