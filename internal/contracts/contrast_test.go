// SPDX-License-Identifier: AGPL-3.0-or-later

package contracts_test

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Contrast is checked here, in Go, rather than with a browser: the tokens are the contract, and a
// contract test that needs a running browser will be skipped the first time it is inconvenient.
// This parses web/src/styles/tokens.css and computes WCAG 2.1 ratios directly, for both themes.
//
// Milestone B1 acceptance criterion. Thresholds are WCAG AA: 4.5:1 for body text, 3:1 for large
// text and for non-text UI indicators such as the status dot.

var hexToken = regexp.MustCompile(`(--v-[a-z0-9-]+):\s*(#[0-9a-fA-F]{6})\s*;`)

type palette map[string]rgb

type rgb struct{ r, g, b float64 }

func parseHex(s string) rgb {
	v, _ := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	return rgb{
		r: float64((v >> 16) & 0xff),
		g: float64((v >> 8) & 0xff),
		b: float64(v & 0xff),
	}
}

// relativeLuminance implements WCAG 2.1's definition, including the sRGB linearisation that a
// naive average-of-channels would get wrong.
func (c rgb) relativeLuminance() float64 {
	lin := func(v float64) float64 {
		v /= 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.r) + 0.7152*lin(c.g) + 0.0722*lin(c.b)
}

func contrast(a, b rgb) float64 {
	la, lb := a.relativeLuminance(), b.relativeLuminance()
	if lb > la {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// palettes returns the light palette (bare :root) and the dark one (:root[data-theme="dark"]),
// with dark inheriting anything it does not override - exactly how the cascade resolves it.
func palettes(t *testing.T) (light, dark palette) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "web/src/styles/tokens.css"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(body)

	block := func(start string) palette {
		i := strings.Index(css, start)
		if i < 0 {
			t.Fatalf("tokens.css has no %q block", start)
		}
		rest := css[i+len(start):]
		end := strings.Index(rest, "\n}")
		if end < 0 {
			t.Fatalf("unterminated %q block", start)
		}
		p := palette{}
		for _, m := range hexToken.FindAllStringSubmatch(rest[:end], -1) {
			p[m[1]] = parseHex(m[2])
		}
		return p
	}

	light = block(":root {")
	dark = palette{}
	for k, v := range light {
		dark[k] = v
	}
	for k, v := range block(`:root[data-theme="dark"] {`) {
		dark[k] = v
	}
	if len(light) < 8 || len(dark) < 8 {
		t.Fatalf("parsed too few tokens (light %d, dark %d); the parser is broken", len(light), len(dark))
	}
	return light, dark
}

func TestTokenContrast(t *testing.T) {
	type pair struct {
		fg, bg string
		min    float64
		why    string
	}
	// Body text must clear 4.5:1. Muted and faint text is still body-sized, so it gets the same
	// bar - "secondary" is not a licence to be unreadable. Status colours are used both as text
	// and as a 7px dot, and the dot is the weaker case at 3:1.
	pairs := []pair{
		{"--v-text", "--v-bg", 4.5, "body text on the page"},
		{"--v-text", "--v-surface", 4.5, "body text on a card"},
		{"--v-text", "--v-surface-2", 4.5, "body text on a nested surface"},
		{"--v-muted", "--v-bg", 4.5, "secondary text on the page"},
		{"--v-muted", "--v-surface", 4.5, "secondary text on a card"},
		{"--v-faint", "--v-surface", 4.5, "tertiary text on a card"},
		{"--v-faint", "--v-bg", 4.5, "section headings and the footer sit on the page, not a card"},
		{"--v-ok", "--v-surface", 3.0, "status dot and label"},
		{"--v-warn", "--v-surface", 3.0, "status dot and label"},
		{"--v-error", "--v-surface", 3.0, "status dot and label"},
		{"--v-accent", "--v-surface", 3.0, "progress fill and focus ring"},
		{"--v-border", "--v-surface", 1.2, "a visible edge between card and page"},
	}

	light, dark := palettes(t)
	for _, theme := range []struct {
		name string
		p    palette
	}{{"light", light}, {"dark", dark}} {
		t.Run(theme.name, func(t *testing.T) {
			for _, pr := range pairs {
				fg, okFg := theme.p[pr.fg]
				bg, okBg := theme.p[pr.bg]
				if !okFg || !okBg {
					t.Fatalf("token missing: %s or %s", pr.fg, pr.bg)
				}
				got := contrast(fg, bg)
				if got < pr.min {
					t.Errorf("%s on %s = %.2f:1, want >= %.1f:1 (%s)",
						pr.fg, pr.bg, got, pr.min, pr.why)
				}
			}
		})
	}
}

// TestTokensUsedNotHardcoded is the constraint spike S4 was built to test: if a component needs a
// colour that is not a token, the vocabulary is wrong. Catching a stray hex early is much cheaper
// than discovering at theme-switch time that one card ignores the palette.
func TestComponentsUseTokensNotHardcodedColours(t *testing.T) {
	root := repoRoot(t)
	src := filepath.Join(root, "web", "src")
	// Colour literals of any form. `transparent`, `currentColor` and `inherit` are keywords, not
	// palette choices, so they are allowed.
	literal := regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b|\brgba?\(|\bhsla?\(`)

	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".svelte") && !strings.HasSuffix(path, ".css") {
			return nil
		}
		if strings.HasSuffix(path, "tokens.css") {
			return nil // the one file allowed to define colours
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(body), "\n") {
			if literal.MatchString(line) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s:%d uses a colour literal; every colour is a token in tokens.css\n    %s",
					rel, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestStaleStateContrast is the regression guard for a real bug B3 found: Card.svelte's `.dimmed`
// class (applied to a card's content when execution.state is "stale") used to combine
// opacity:0.55 with a saturate() filter. That looked like a modest dim in isolation, but
// --v-faint already sits at 4.71:1 against --v-surface - barely above the WCAG AA floor for body
// text - and ANY opacity reduction pushes a composited near-grey token below 4.5:1: even
// opacity:0.9 fails. No contract test caught it at the time because TestTokenContrast (above)
// checks the BASE palette only, never a CSS transform applied on top of it - and it was only
// found by actually rendering the stale card with real content and looking at it, which is the
// argument for doing exactly that at every UI milestone rather than trusting typecheck and unit
// tests alone.
//
// This test parses the REAL `.dimmed` rule from Card.svelte - not a hardcoded assumption of what
// it says - simulates the same pixel pipeline a browser applies (filter, THEN opacity
// compositing over the backdrop), and asserts the result still clears AA for every neutral text
// token in both palettes. Reintroducing opacity on this rule fails this test again.
func TestStaleStateContrast(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "web/src/lib/Card.svelte"))
	if err != nil {
		t.Fatal(err)
	}
	css := string(body)

	i := strings.Index(css, ".dimmed {")
	if i < 0 {
		t.Fatal("Card.svelte has no .dimmed rule; either it was renamed (update this test) or the " +
			"stale-state dimming was removed entirely (update docs/01-architecture.md section 4 too)")
	}
	end := strings.Index(css[i:], "}")
	if end < 0 {
		t.Fatal("unterminated .dimmed rule")
	}
	rule := css[i : i+end]

	opacity := 1.0
	if m := regexp.MustCompile(`opacity:\s*([\d.]+)`).FindStringSubmatch(rule); m != nil {
		opacity, _ = strconv.ParseFloat(m[1], 64)
	}
	saturation := 1.0
	if m := regexp.MustCompile(`saturate\(([\d.]+)\)`).FindStringSubmatch(rule); m != nil {
		saturation, _ = strconv.ParseFloat(m[1], 64)
	}

	light, dark := palettes(t)
	neutralTokens := []string{"--v-text", "--v-muted", "--v-faint"}

	for _, theme := range []struct {
		name string
		p    palette
		bg   string
	}{{"light", light, "--v-surface"}, {"dark", dark, "--v-surface"}} {
		t.Run(theme.name, func(t *testing.T) {
			bg, ok := theme.p[theme.bg]
			if !ok {
				t.Fatalf("token %s missing", theme.bg)
			}
			for _, tok := range neutralTokens {
				fg, ok := theme.p[tok]
				if !ok {
					t.Fatalf("token %s missing", tok)
				}
				rendered := fg.saturate(saturation).compositeOver(bg, opacity)
				got := contrast(rendered, bg)
				if got < 4.5 {
					t.Errorf("%s at opacity=%.2f, saturate=%.2f on %s = %.2f:1, want >= 4.5:1 "+
						"(stale content must stay legible, not merely present)",
						tok, opacity, saturation, theme.bg, got)
				}
			}
		})
	}
}

// saturate applies the CSS saturate() filter: pixels move toward their own luminance-weighted
// grey by a factor (1.0 = unchanged, 0.0 = fully desaturated). Matches the SVG/CSS filter
// specification, not a naive per-channel scale.
func (c rgb) saturate(factor float64) rgb {
	gray := 0.2126*c.r + 0.7152*c.g + 0.0722*c.b
	clamp := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return v
	}
	return rgb{
		r: clamp(gray + (c.r-gray)*factor),
		g: clamp(gray + (c.g-gray)*factor),
		b: clamp(gray + (c.b-gray)*factor),
	}
}

// compositeOver simulates CSS opacity: alpha-blending this colour over a background at the given
// opacity (1.0 = fully this colour, 0.0 = fully the background) - what a browser actually paints,
// which is what contrast must be measured against, not the token's own nominal colour.
func (c rgb) compositeOver(bg rgb, opacity float64) rgb {
	return rgb{
		r: opacity*c.r + (1-opacity)*bg.r,
		g: opacity*c.g + (1-opacity)*bg.g,
		b: opacity*c.b + (1-opacity)*bg.b,
	}
}
