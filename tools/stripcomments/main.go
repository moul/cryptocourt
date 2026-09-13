// Strip comments from .gno files so a large realm fits the 1MB JSON-RPC body
// limit (the tx is base64-encoded on the wire, so source must stay under ~750KB).
//
// Text-level stripping is not safe: backticks inside comments desynchronise any
// raw-string tracker, and `//` appears inside string literals. So this tokenises
// with go/scanner, which gets both right, and rebuilds from the token stream.
//
// The -verify pass is the actual guarantee: it tokenises the original and the
// stripped file and asserts the two non-comment token streams are identical.
// If stripping changed anything but comments, that comparison fails.
package main

import (
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// tokens returns the non-comment token stream, each as "pos-independent" text.
func tokens(src []byte, name string) ([]string, error) {
	var s scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile(name, fset.Base(), len(src))
	var errs scanner.ErrorList
	s.Init(file, src, func(pos token.Position, msg string) {
		errs.Add(pos, msg)
	}, 0) // mode 0 = do NOT emit comments
	var out []string
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if lit != "" {
			out = append(out, tok.String()+" "+lit)
		} else {
			out = append(out, tok.String())
		}
	}
	if errs.Len() > 0 {
		return nil, errs.Err()
	}
	return out, nil
}

// strip rebuilds src without comments, preserving line structure so that any
// panic line numbers and the general shape of the file stay recognisable.
func strip(src []byte, name string) ([]byte, error) {
	var s scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile(name, fset.Base(), len(src))
	var errs scanner.ErrorList
	s.Init(file, src, func(pos token.Position, msg string) {
		errs.Add(pos, msg)
	}, scanner.ScanComments)

	// Collect byte ranges of every comment.
	type span struct{ lo, hi int }
	var spans []span
	base := file.Base()
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.COMMENT {
			lo := int(pos) - base
			spans = append(spans, span{lo, lo + len(lit)})
		}
	}
	if errs.Len() > 0 {
		return nil, errs.Err()
	}

	// Blank out comment bytes, then drop lines that became empty.
	keep := make([]byte, len(src))
	copy(keep, src)
	for _, sp := range spans {
		for i := sp.lo; i < sp.hi && i < len(keep); i++ {
			keep[i] = 0
		}
	}
	var b strings.Builder
	for _, line := range strings.Split(string(keep), "\n") {
		line = strings.ReplaceAll(line, "\x00", "")
		if strings.TrimSpace(line) == "" {
			continue // was a whole-line comment, or already blank
		}
		b.WriteString(strings.TrimRight(line, " \t"))
		b.WriteByte('\n')
	}
	return []byte(b.String()), nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: stripcomments <dir> [<dir>...]")
		os.Exit(2)
	}
	var before, after, nfiles int
	for _, dir := range os.Args[1:] {
		err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".gno") {
				return err
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out, err := strip(src, p)
			if err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			// VERIFY: the two token streams must be identical.
			a, err := tokens(src, p)
			if err != nil {
				return fmt.Errorf("%s (original): %w", p, err)
			}
			c, err := tokens(out, p)
			if err != nil {
				return fmt.Errorf("%s (stripped): %w", p, err)
			}
			if len(a) != len(c) {
				return fmt.Errorf("%s: token count changed %d -> %d", p, len(a), len(c))
			}
			for i := range a {
				if a[i] != c[i] {
					return fmt.Errorf("%s: token %d changed %q -> %q", p, i, a[i], c[i])
				}
			}
			before += len(src)
			after += len(out)
			nfiles++
			return os.WriteFile(p, out, fi.Mode())
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "FAILED:", err)
			os.Exit(1)
		}
	}
	fmt.Printf("%d files: %d -> %d bytes (%.0f%% smaller), token streams identical\n",
		nfiles, before, after, 100*float64(before-after)/float64(before))
	fmt.Printf("base64 on the wire: %d bytes (limit 1000000)\n", after*4/3)
}
