package archive

import "testing"

func TestUnescapeMarkdown(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		// What the realm actually stores, and what a reader typed.
		{`Virology\.`, "Virology."},
		{`Gain\-of\-function funding`, "Gain-of-function funding"},
		{`82% of completed pregnancies\.`, "82% of completed pregnancies."},
		{`nothing to undo`, "nothing to undo"},
		// A BACKSLASH IS A LEGITIMATE CHARACTER and must survive when it is not
		// an escape. A blanket strip would eat these.
		{`C:\Users\jae`, `C:\Users\jae`},
		{`a\\b`, `a\b`}, // an escaped backslash IS an escape; one survives
		{`trailing \`, `trailing \`},
	} {
		if got := unescapeMarkdown(c.in); got != c.want {
			t.Errorf("unescapeMarkdown(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
