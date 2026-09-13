// SPDX-License-Identifier: AGPL-3.0-or-later

package contracts_test

import (
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestVeilContrast is TestTokenContrast for the translucent preset, and it exists because the
// opaque test structurally cannot cover Veil: TestTokenContrast reads hex pairs, but Veil's
// --v-surface is `rgb(var(--v-veil-tint) / var(--v-veil-alpha))`, which has no luminance until it
// is composited over whatever is behind it.
//
// This covers the bottom layer of Veil's backdrop: the GENERATED gradient, which is what the page
// falls back to when the shipped painting above it has not loaded or 404s. The two stops
// interpolate each sRGB channel monotonically, and relative luminance is monotonic in each
// channel, so composited luminance along the whole gradient is bounded by its value at the two
// stops - evaluating both is therefore exhaustive, not a sample. Blur does not widen that range: a
// gradient is already low-frequency.
//
// So the preset stays legible even with no image at all, which is the case no image-based test can
// reach. A painting - shipped or configured - has no such bound, which is why it sits under a
// mandatory scrim evaluated worst-case by TestVeilImageContrast instead. See
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

			// The scrim sits between the gradient and the card, in the no-image case exactly as
			// in the image case: it is one declaration in the base Veil block, not something the
			// backdrop layer chooses. Compositing it here is therefore not extra rigour, it is
			// what the browser does.
			scrimAlpha := scalarIn(t, veilLight, "--v-scrim-alpha")
			scrim := tripleIn(t, firstWith(veilLight, body, "--v-scrim"), "--v-scrim")
			surfaces := []rgb{
				composite(tint, alpha, composite(scrim, scrimAlpha, from)),
				composite(tint, alpha, composite(scrim, scrimAlpha, to)),
			}

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

// TestVeilImageContrast is the guarantee for the case arithmetic cannot bound on its own: a
// painting, whose pixels the palette cannot be reasoned about from. That is true of the two
// vedute Veil ships as well as of a background an operator configures - dashboard.background
// replaces the image URL and changes nothing else, so one test covers both.
//
// The gradient fallback is bounded by its two stops. A painting is bounded by nothing, so the
// contract is enforced instead by the mandatory scrim in tokens.css: the image contributes at
// most (1 - --v-scrim-alpha) of the backdrop. This test therefore evaluates the extremes an image
// can actually present - pure black and pure white - rather than the two paintings that happen to
// ship today, which would say nothing about the next background an operator configures.
//
// Both extremes matter and for opposite reasons: in dark the scrim caps composited luminance from
// above, because light text needs the backdrop to stay dark, and a white image is the adversary;
// in light it imposes a floor, and a black image is. Testing only one would leave half the
// contract unproven.
func TestVeilImageContrast(t *testing.T) {
	light, dark := palettes(t)
	css := tokensCSS(t)
	veilLight := blockBody(t, css, `:root[data-appearance="veil"] {`)
	veilDark := blockBody(t, css, `:root[data-appearance="veil"]:not([data-theme="light"]) {`)

	surfaceAlpha := scalarIn(t, veilLight, "--v-veil-alpha")
	scrimAlpha := scalarIn(t, veilLight, "--v-scrim-alpha")

	// The worst an image can be, in both directions. Nothing between these is worse.
	extremes := map[string]rgb{"pure black": {r: 0, g: 0, b: 0}, "pure white": {r: 255, g: 255, b: 255}}

	for _, scheme := range []struct {
		name  string
		base  palette
		block string
	}{
		{"light", light, veilLight},
		{"dark", dark, veilDark},
	} {
		t.Run(scheme.name, func(t *testing.T) {
			p := palette{}
			for k, v := range scheme.base {
				p[k] = v
			}
			for k, v := range hexTokensIn(veilLight) {
				p[k] = v
			}
			if scheme.name == "dark" {
				for k, v := range hexTokensIn(veilDark) {
					p[k] = v
				}
			}
			tint := tripleIn(t, firstWith(veilLight, scheme.block, "--v-veil-tint"), "--v-veil-tint")
			scrim := tripleIn(t, firstWith(veilLight, scheme.block, "--v-scrim"), "--v-scrim")

			for _, pr := range []struct {
				tok string
				min float64
			}{
				{"--v-text", 4.5}, {"--v-muted", 4.5}, {"--v-faint", 4.5},
				{"--v-ok", 3.0}, {"--v-warn", 3.0}, {"--v-error", 3.0},
				{"--v-accent", 3.0}, {"--v-border", 1.2},
			} {
				fg, ok := p[pr.tok]
				if !ok {
					t.Fatalf("token missing: %s", pr.tok)
				}
				for label, image := range extremes {
					backdrop := composite(scrim, scrimAlpha, image)
					surface := composite(tint, surfaceAlpha, backdrop)
					if got := contrast(fg, surface); got < pr.min {
						t.Errorf("%s over a %s background image (scrim %.2f, surface %.2f) = %.2f:1, want >= %.1f:1",
							pr.tok, label, scrimAlpha, surfaceAlpha, got, pr.min)
					}
				}
			}
		})
	}
}

// TestVeilConfiguredBackgroundKeepsTheScrim is the structural half of the guarantee above.
//
// That proof holds for a configured background only because configuring one swaps the image URL
// and nothing else. An earlier version of tokens.css restated the whole background shorthand -
// scrim included - in the [data-backdrop="image"] block, which meant the scrim existed twice and
// could be weakened in one copy while every contrast test kept reading the other and passing.
func TestVeilConfiguredBackgroundKeepsTheScrim(t *testing.T) {
	css := tokensCSS(t)
	block := blockBody(t, css, `:root[data-appearance="veil"][data-backdrop="image"] {`)

	if !strings.Contains(block, "--v-backdrop-image:") {
		t.Error("the configured-background block does not override --v-backdrop-image")
	}
	if !strings.Contains(block, "/api/v1/background") {
		t.Error("the configured background does not resolve to the server's own route")
	}
	for _, token := range []string{"--v-scrim", "--v-scrim-alpha", "--v-veil-alpha", "--v-veil-tint"} {
		if strings.Contains(block, token+":") {
			t.Errorf("the configured-background block redeclares %s; the contrast contract is proven "+
				"against the one declaration in the base Veil block, so a second copy can silently drift", token)
		}
	}
}

// TestBundledBackdropTone holds the other half of what makes the shipped paintings usable as a
// backdrop: not that they are legible - the scrim proves that for any image - but that they are
// CALM. A dashboard is read, not looked at, and a full-strength history painting behind it
// competes with the cards for the same attention.
//
// So the two files are shipped pre-toned: desaturated and contrast-compressed toward the scheme's
// own scrim colour when they were encoded, rather than filtered in CSS where the cost would be a
// composited layer on every paint and the result could not be asserted. This test is what makes
// that a property of the repository instead of a one-time decision someone made in an image
// editor: replace either file with a harsh original and it fails.
//
// It measures the COMPOSITED backdrop - scrim over image, at the shipped alpha - because that is
// what a viewer actually sees, and it is the quantity "distracting" is a claim about. Percentiles
// rather than min/max: a single specular highlight (Vernet's moon is exactly that) is not a
// distraction, a bright half of the frame is.
func TestBundledBackdropTone(t *testing.T) {
	css := tokensCSS(t)
	veilLight := blockBody(t, css, `:root[data-appearance="veil"] {`)
	veilDark := blockBody(t, css, `:root[data-appearance="veil"]:not([data-theme="light"]) {`)
	scrimAlpha := scalarIn(t, veilLight, "--v-scrim-alpha")

	for _, scheme := range []struct {
		name string
		// The composited backdrop's p02-p98 luminance span: how much the page's background moves
		// across the frame. The yardstick is the generated gradient each painting replaced, which
		// composites to a span of about 0.07 in light and far less in dark - so these bounds say
		// "no busier than the backdrop that shipped before", which is a claim about this design
		// rather than a number chosen to fit the files.
		maxSpan float64
		// Which side of the range the backdrop must sit on, so it reads as this scheme's page
		// rather than as a picture that happens to be behind one.
		minMedian, maxMedian float64
		// Mean sRGB saturation of the file itself. A desaturated painting cannot introduce a hue
		// that competes with --v-accent, which is the dashboard's only colour with a job.
		maxSaturation float64
		block         string
	}{
		{"light", 0.11, 0.55, 0.85, 0.12, veilLight},
		{"dark", 0.010, 0.0, 0.10, 0.30, veilDark},
	} {
		t.Run(scheme.name, func(t *testing.T) {
			file := backdropFile(t, firstWith(veilLight, scheme.block, "--v-backdrop-image"))
			scrim := tripleIn(t, firstWith(veilLight, scheme.block, "--v-scrim"), "--v-scrim")

			lums, saturation := backdropTone(t, file, scrim, scrimAlpha)
			p := func(q float64) float64 { return lums[int(q*float64(len(lums)-1))] }
			span := p(0.98) - p(0.02)
			t.Logf("%s composited over the %s scrim: luminance p02 %.4f, median %.4f, p98 %.4f (span %.4f), mean saturation %.4f",
				file, scheme.name, p(0.02), p(0.50), p(0.98), span, saturation)

			if span > scheme.maxSpan {
				t.Errorf("%s: composited backdrop luminance spans %.4f (p02 %.4f, p98 %.4f), want <= %.4f - "+
					"the backdrop moves too much across the frame to sit behind text",
					file, span, p(0.02), p(0.98), scheme.maxSpan)
			}
			if median := p(0.50); median < scheme.minMedian || median > scheme.maxMedian {
				t.Errorf("%s: composited backdrop median luminance %.4f, want within [%.2f, %.2f] for the %s scheme",
					file, median, scheme.minMedian, scheme.maxMedian, scheme.name)
			}
			if saturation > scheme.maxSaturation {
				t.Errorf("%s: mean saturation %.4f, want <= %.4f - the shipped file is not toned down",
					file, saturation, scheme.maxSaturation)
			}
		})
	}
}

// backdropUrl pulls the bundled file out of `--v-backdrop-image: url("./backdrops/x.jpg");`. It
// deliberately does not accept the configured-background route: there is no file to measure there,
// which is the whole reason the scrim exists.
var backdropUrl = regexp.MustCompile(`--v-backdrop-image:\s*url\("\./([A-Za-z0-9._/-]+)"\)\s*;`)

func backdropFile(t *testing.T, body string) string {
	t.Helper()
	m := backdropUrl.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no bundled --v-backdrop-image in this block")
	}
	return m[1]
}

// backdropTone returns the sorted per-pixel relative luminance of the backdrop as composited -
// scrim over image - together with the mean sRGB saturation of the image itself.
func backdropTone(t *testing.T, file string, scrim rgb, scrimAlpha float64) (lums []float64, saturation float64) {
	t.Helper()
	path := filepath.Join(repoRoot(t), "web/src/styles", file)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	img, err := jpeg.Decode(f)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}

	b := img.Bounds()
	lums = make([]float64, 0, b.Dx()*b.Dy())
	var total float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r16, g16, b16, _ := img.At(x, y).RGBA()
			px := rgb{r: float64(r16 >> 8), g: float64(g16 >> 8), b: float64(b16 >> 8)}
			lums = append(lums, composite(scrim, scrimAlpha, px).relativeLuminance())

			high := math.Max(px.r, math.Max(px.g, px.b))
			if high > 0 {
				total += (high - math.Min(px.r, math.Min(px.g, px.b))) / high
			}
		}
	}
	sort.Float64s(lums)
	return lums, total / float64(len(lums))
}
