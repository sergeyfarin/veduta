// SPDX-License-Identifier: AGPL-3.0-or-later

package contracts_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestVeilContrast is TestTokenContrast for the translucent preset, and it exists because the
// opaque test structurally cannot cover Veil: TestTokenContrast reads hex pairs, but Veil's
// --v-surface is `rgb(var(--v-veil-tint) / var(--v-veil-alpha))`, which has no luminance until it
// is composited over whatever is behind it.
//
// What makes that computable is the choice of a GENERATED gradient as the default backdrop rather
// than a shipped photograph. The two stops interpolate each sRGB channel monotonically, and
// relative luminance is monotonic in each channel, so composited luminance along the whole
// gradient is bounded by its value at the two stops - evaluating both is therefore exhaustive,
// not a sample. Blur does not widen that range: a gradient is already low-frequency.
//
// This is the guarantee the ADR claims and would otherwise be an assertion nobody checks. A
// user-supplied photograph has no such bound, which is why that case is specified to need a scrim
// and worst-case (pure black / pure white) evaluation instead - see
// docs/decisions/0002-theming-and-visual-customisation.md.
func TestVeilContrast(t *testing.T) {
	light, dark := palettes(t)
	css := tokensCSS(t)

	for _, scheme := range []struct {
		name     string
		base     palette
		selector string
	}{
		{"light", light, `:root[data-appearance="veil"] {`},
		{"dark", dark, `:root[data-appearance="veil"]:not([data-theme="light"]) {`},
	} {
		t.Run(scheme.name, func(t *testing.T) {
			// Veil overrides only what it must; everything else inherits from the opaque palette,
			// exactly how the cascade resolves it.
			p := palette{}
			for k, v := range scheme.base {
				p[k] = v
			}
			veilLight := blockBody(t, css, `:root[data-appearance="veil"] {`)
			body := blockBody(t, css, scheme.selector)
			for k, v := range hexTokensIn(veilLight) {
				p[k] = v
			}
			if scheme.name == "dark" {
				for k, v := range hexTokensIn(body) {
					p[k] = v
				}
			}

			// The tint and alpha are Veil's own; the dark block overrides the tint.
			alpha := scalarIn(t, veilLight, "--v-veil-alpha")
			tint := tripleIn(t, firstWith(veilLight, body, "--v-veil-tint"), "--v-veil-tint")
			from, ok := p["--v-backdrop-from"]
			if !ok {
				t.Fatal("--v-backdrop-from missing")
			}
			to, ok := p["--v-backdrop-to"]
			if !ok {
				t.Fatal("--v-backdrop-to missing")
			}

			surfaces := []rgb{composite(tint, alpha, from), composite(tint, alpha, to)}

			// The same floors TestTokenContrast enforces on the opaque palette. Body text at
			// 4.5:1, status and accent at 3:1 - a preset does not get an accessibility discount
			// for being pretty.
			for _, pr := range []struct {
				tok string
				min float64
				why string
			}{
				{"--v-text", 4.5, "body text on a translucent card"},
				{"--v-muted", 4.5, "secondary text on a translucent card"},
				{"--v-faint", 4.5, "tertiary text on a translucent card"},
				{"--v-ok", 3.0, "status dot and label"},
				{"--v-warn", 3.0, "status dot and label"},
				{"--v-error", 3.0, "status dot and label"},
				{"--v-accent", 3.0, "progress fill and focus ring"},
				{"--v-border", 1.2, "a visible edge between card and backdrop"},
			} {
				fg, ok := p[pr.tok]
				if !ok {
					t.Fatalf("token missing: %s", pr.tok)
				}
				for i, surface := range surfaces {
					stop := "backdrop-from"
					if i == 1 {
						stop = "backdrop-to"
					}
					if got := contrast(fg, surface); got < pr.min {
						t.Errorf("%s over %s composited at alpha %.2f = %.2f:1, want >= %.1f:1 (%s)",
							pr.tok, stop, alpha, got, pr.min, pr.why)
					}
				}
			}
		})
	}
}

func tokensCSS(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "web/src/styles/tokens.css"))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func blockBody(t *testing.T, css, start string) string {
	t.Helper()
	i := strings.Index(css, start)
	if i < 0 {
		t.Fatalf("tokens.css has no %q block", start)
	}
	rest := css[i+len(start):]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatalf("unterminated %q block", start)
	}
	return rest[:end]
}

func hexTokensIn(body string) palette {
	p := palette{}
	for _, m := range hexToken.FindAllStringSubmatch(body, -1) {
		p[m[1]] = parseHex(m[2])
	}
	return p
}

var scalarToken = regexp.MustCompile(`(--v-[a-z0-9-]+):\s*([0-9.]+)\s*;`)
var tripleToken = regexp.MustCompile(`(--v-[a-z0-9-]+):\s*(\d{1,3})\s+(\d{1,3})\s+(\d{1,3})\s*;`)

func scalarIn(t *testing.T, body, name string) float64 {
	t.Helper()
	for _, m := range scalarToken.FindAllStringSubmatch(body, -1) {
		if m[1] == name {
			v, err := strconv.ParseFloat(m[2], 64)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			return v
		}
	}
	t.Fatalf("%s not found", name)
	return 0
}

func tripleIn(t *testing.T, body, name string) rgb {
	t.Helper()
	for _, m := range tripleToken.FindAllStringSubmatch(body, -1) {
		if m[1] == name {
			n := func(s string) float64 {
				v, err := strconv.ParseFloat(s, 64)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				return v
			}
			return rgb{r: n(m[2]), g: n(m[3]), b: n(m[4])}
		}
	}
	t.Fatalf("%s not found", name)
	return rgb{}
}

// firstWith returns whichever body declares name, preferring the override block.
func firstWith(base, override, name string) string {
	if tripleToken.MatchString(override) && strings.Contains(override, name) {
		return override
	}
	return base
}

// composite is source-over alpha compositing, the same operation the browser performs when a
// translucent surface paints on the backdrop beneath it.
func composite(tint rgb, alpha float64, backdrop rgb) rgb {
	return rgb{
		r: alpha*tint.r + (1-alpha)*backdrop.r,
		g: alpha*tint.g + (1-alpha)*backdrop.g,
		b: alpha*tint.b + (1-alpha)*backdrop.b,
	}
}
