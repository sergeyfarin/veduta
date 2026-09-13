// SPDX-License-Identifier: AGPL-3.0-or-later

package contracts_test

import (
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Veil is TestTokenContrast's problem restated, and the opaque test structurally cannot cover it:
// TestTokenContrast reads hex pairs, but Veil's --v-surface is
// `rgb(var(--v-veil-tint) / var(--v-veil-alpha))`, which has no luminance until it is composited
// over whatever is behind it.
//
// Two things make that computable, and they are different for the two backdrops Veil can have:
//
//   - The BUNDLED paintings are files in this repository, so their pixels are known here and the
//     floors are proven against what they actually contain (TestVeilContrast).
//   - A CONFIGURED dashboard.background is an arbitrary file, so the floors can only be proven
//     against the worst an image can be (TestVeilImageContrast).
//
// That difference is why the two cases carry different scrim strengths, and it is the whole reason
// the preset can be transparent enough to see through at all. See tokens.css.
//
// Both tests share two pieces of care:
//
//   - Contrast is V-SHAPED in backdrop luminance: it is worst where the surface luminance is
//     closest to the text's, which is not necessarily an extreme. Checking only the extremes is
//     sound only while every token sits outside the reachable range, and nothing was enforcing
//     that assumption. worstContrast clamps instead, which is exact either way.
//   - The bound is taken per CHANNEL, not per pixel, because backdrop-filter blurs the backdrop
//     behind each card. A blurred sample is a convex combination of neighbouring pixels in each
//     channel independently, so it can pair one pixel's red with another's blue; the
//     all-minimums and all-maximums corners bound every blur radius exactly, and cheaply.

// veilFloors is the contrast matrix, identical to the one TestTokenContrast enforces on the
// opaque palette. A preset does not get an accessibility discount for being pretty.
var veilFloors = []struct {
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
}

// TestVeilContrast proves the floors against the paintings Veil actually ships, pixel by pixel.
//
// This is what lets the bundled backdrop run at a scrim of 0.18 rather than the 0.70 an unknown
// image needs: the file is in the repository, so "what can this backdrop do to the text" is a
// question with an answer here rather than a worst case to defend against. Re-tone either painting
// harshly and this fails, which is the point - the toning is load-bearing, not decoration.
func TestVeilContrast(t *testing.T) {
	css := tokensCSS(t)
	for _, scheme := range veilSchemes(t) {
		t.Run(scheme.name, func(t *testing.T) {
			file := backdropFile(t, scheme.block)
			lo, hi := backdropChannelBox(t, file)
			s := scheme.surfaces(t, css, lo, hi)

			for _, f := range veilFloors {
				fg, ok := scheme.palette[f.tok]
				if !ok {
					t.Fatalf("token missing: %s", f.tok)
				}
				if got := worstContrast(fg, s.minL, s.maxL); got < f.min {
					t.Errorf("%s over %s = %.2f:1 at worst, want >= %.1f:1 (%s)",
						f.tok, file, got, f.min, f.why)
				}
			}

			// Nested panels (--v-surface-2) are more opaque than the card, so text on them is
			// strictly safer. Asserted rather than argued: the day someone makes surface-2 the
			// more transparent of the two, this is what says so.
			if got := worstContrast(scheme.palette["--v-text"], s.nestedMinL, s.nestedMaxL); got < 4.5 {
				t.Errorf("--v-text on --v-surface-2 over %s = %.2f:1 at worst, want >= 4.5:1", file, got)
			}
		})
	}
}

// TestVeilImageContrast is the guarantee for the case arithmetic cannot bound on its own: a
// dashboard.background an operator configures, whose pixels are unknown here.
//
// It evaluates the extremes an image can actually present - pure black and pure white - rather
// than a representative photograph, which would say nothing about the next one. Both extremes
// matter and for opposite reasons: in dark the scrim caps composited luminance from above, because
// light text needs the backdrop to stay dark, and a white image is the adversary; in light it
// imposes a floor, and a black image is.
func TestVeilImageContrast(t *testing.T) {
	css := tokensCSS(t)
	configured := blockBody(t, css, `:root[data-appearance="veil"][data-backdrop="image"] {`)
	scrimAlpha := scalarIn(t, configured, "--v-scrim-alpha")

	for _, scheme := range veilSchemes(t) {
		t.Run(scheme.name, func(t *testing.T) {
			black, white := rgb{r: 0, g: 0, b: 0}, rgb{r: 255, g: 255, b: 255}
			s := scheme.surfacesAt(t, css, scrimAlpha, black, white)

			for _, f := range veilFloors {
				fg, ok := scheme.palette[f.tok]
				if !ok {
					t.Fatalf("token missing: %s", f.tok)
				}
				if got := worstContrast(fg, s.minL, s.maxL); got < f.min {
					t.Errorf("%s over a configured background image (scrim %.2f) = %.2f:1 at worst, "+
						"want >= %.1f:1 (%s)", f.tok, scrimAlpha, got, f.min, f.why)
				}
			}
			if got := worstContrast(scheme.palette["--v-text"], s.nestedMinL, s.nestedMaxL); got < 4.5 {
				t.Errorf("--v-text on --v-surface-2 over a configured background = %.2f:1 at worst, want >= 4.5:1", got)
			}
		})
	}
}

// TestVeilFallbackGradientIsInsideTheBackdrop is how the gradient under the painting gets a proof
// without needing its own.
//
// The gradient renders when the painting has not loaded or 404s - the one case no image-based test
// reaches. Rather than run the whole matrix a third time against its two stops, the stops are
// required to lie inside the painting's own channel box: compositing and relative luminance are
// monotone in each channel, so anything inside that box produces a surface inside the range
// TestVeilContrast already cleared. Move a stop outside the box and this fails, which is the
// reminder that the fallback then needs proving on its own terms.
func TestVeilFallbackGradientIsInsideTheBackdrop(t *testing.T) {
	for _, scheme := range veilSchemes(t) {
		t.Run(scheme.name, func(t *testing.T) {
			file := backdropFile(t, scheme.block)
			lo, hi := backdropChannelBox(t, file)
			for _, stop := range []string{"--v-backdrop-from", "--v-backdrop-to"} {
				c, ok := scheme.palette[stop]
				if !ok {
					t.Fatalf("token missing: %s", stop)
				}
				for _, ch := range []struct {
					name         string
					v, low, high float64
				}{
					{"red", c.r, lo.r, hi.r},
					{"green", c.g, lo.g, hi.g},
					{"blue", c.b, lo.b, hi.b},
				} {
					if ch.v < ch.low || ch.v > ch.high {
						t.Errorf("%s %s channel %.0f is outside %s's range [%.0f, %.0f]; the fallback "+
							"gradient no longer inherits the painting's contrast proof",
							stop, ch.name, ch.v, file, ch.low, ch.high)
					}
				}
			}
		})
	}
}

// TestVeilConfiguredBackgroundRaisesTheScrim is the structural half of the two-scrim design.
//
// Configuring a background must change the image and the scrim over it, and nothing else. An
// earlier tokens.css restated the whole background shorthand here, which meant the scrim existed
// twice and could be weakened in one copy while every contrast test kept reading the other and
// passing. The direction matters too: an unknown image can only ever need MORE scrim than the
// bundled one, never less.
func TestVeilConfiguredBackgroundRaisesTheScrim(t *testing.T) {
	css := tokensCSS(t)
	bundled := blockBody(t, css, `:root[data-appearance="veil"] {`)
	configured := blockBody(t, css, `:root[data-appearance="veil"][data-backdrop="image"] {`)

	if !strings.Contains(configured, "/api/v1/background") {
		t.Error("the configured background does not resolve to the server's own route")
	}
	if got, want := scalarIn(t, configured, "--v-scrim-alpha"), scalarIn(t, bundled, "--v-scrim-alpha"); got <= want {
		t.Errorf("configured scrim alpha %.2f is not above the bundled %.2f; an image whose pixels "+
			"are unknown here cannot be defended with less scrim than one whose pixels are in the repo",
			got, want)
	}
	for _, token := range []string{"--v-scrim", "--v-veil-alpha", "--v-veil-tint", "--v-surface", "--v-surface-2"} {
		if strings.Contains(configured, token+":") {
			t.Errorf("the configured-background block redeclares %s; the contrast contract is proven "+
				"against the one declaration in the base Veil block, so a second copy can silently drift", token)
		}
	}
}

// TestBundledBackdropTone holds what the contrast tests cannot say: not that the paintings are
// legible behind - TestVeilContrast proves that for the files as they stand - but that they are
// legible behind BY DESIGN, because they were toned, rather than by luck of which reproduction
// someone downloaded.
//
// Swap in a full-strength original and TestVeilContrast fails somewhere specific and confusing;
// this fails first, saying the file is not toned. It measures the file's own channel range and
// saturation, which is what the toning controls and what every other bound here derives from.
func TestBundledBackdropTone(t *testing.T) {
	for _, scheme := range veilSchemes(t) {
		t.Run(scheme.name, func(t *testing.T) {
			file := backdropFile(t, scheme.block)
			lo, hi := backdropChannelBox(t, file)
			saturation := backdropSaturation(t, file)

			// Range of the file itself, as a fraction of the full 0-255 scale. A painting that
			// still spans most of the scale has not been compressed, whatever its median says.
			span := math.Max(hi.r-lo.r, math.Max(hi.g-lo.g, hi.b-lo.b)) / 255
			t.Logf("%s: channel box #%02x%02x%02x..#%02x%02x%02x, widest channel spans %.2f of the scale, mean saturation %.3f",
				file, int(lo.r), int(lo.g), int(lo.b), int(hi.r), int(hi.g), int(hi.b), span, saturation)

			if span > 0.50 {
				t.Errorf("%s: widest channel spans %.2f of the scale, want <= 0.50 - the file is not "+
					"contrast-compressed, so the card surface will swing further than the palette allows", file, span)
			}
			// A desaturated painting cannot introduce a hue that competes with --v-accent, the
			// dashboard's only colour with a job.
			if saturation > 0.28 {
				t.Errorf("%s: mean saturation %.3f, want <= 0.28 - the file is not desaturated", file, saturation)
			}
		})
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────────────────────

type veilScheme struct {
	name    string
	block   string  // the scheme's own Veil block body, where its overrides live
	palette palette // opaque base, plus Veil's light overrides, plus this scheme's
}

// veilSchemes resolves the cascade the way the browser does: the opaque palette, then Veil's base
// block, then - for dark - Veil's dark block on top.
func veilSchemes(t *testing.T) []veilScheme {
	t.Helper()
	light, dark := palettes(t)
	css := tokensCSS(t)
	veilLight := blockBody(t, css, `:root[data-appearance="veil"] {`)
	veilDark := blockBody(t, css, `:root[data-appearance="veil"]:not([data-theme="light"]) {`)

	build := func(base palette, extra string) palette {
		p := palette{}
		for k, v := range base {
			p[k] = v
		}
		for k, v := range hexTokensIn(veilLight) {
			p[k] = v
		}
		if extra != "" {
			for k, v := range hexTokensIn(extra) {
				p[k] = v
			}
		}
		return p
	}
	return []veilScheme{
		{"light", veilLight, build(light, "")},
		{"dark", veilDark, build(dark, veilDark)},
	}
}

// surfaceRange is the reachable luminance of the card surface, and of a nested panel on it.
type surfaceRange struct{ minL, maxL, nestedMinL, nestedMaxL float64 }

func (s veilScheme) surfaces(t *testing.T, css string, lo, hi rgb) surfaceRange {
	t.Helper()
	veilLight := blockBody(t, css, `:root[data-appearance="veil"] {`)
	return s.surfacesAt(t, css, scalarIn(t, veilLight, "--v-scrim-alpha"), lo, hi)
}

func (s veilScheme) surfacesAt(t *testing.T, css string, scrimAlpha float64, lo, hi rgb) surfaceRange {
	t.Helper()
	veilLight := blockBody(t, css, `:root[data-appearance="veil"] {`)
	cardAlpha := scalarIn(t, veilLight, "--v-veil-alpha")
	nestedAlpha := surfaceTwoAlpha(t, veilLight)
	tint := tripleIn(t, firstWith(veilLight, s.block, "--v-veil-tint"), "--v-veil-tint")
	scrim := tripleIn(t, firstWith(veilLight, s.block, "--v-scrim"), "--v-scrim")

	surface := func(backdrop rgb) (card, nested float64) {
		c := composite(tint, cardAlpha, composite(scrim, scrimAlpha, backdrop))
		return c.relativeLuminance(), composite(tint, nestedAlpha, c).relativeLuminance()
	}
	cardLo, nestedLo := surface(lo)
	cardHi, nestedHi := surface(hi)
	return surfaceRange{
		minL: math.Min(cardLo, cardHi), maxL: math.Max(cardLo, cardHi),
		nestedMinL: math.Min(nestedLo, nestedHi), nestedMaxL: math.Max(nestedLo, nestedHi),
	}
}

// worstContrast is the lowest ratio a foreground can reach against any surface luminance in
// [minL, maxL]. Contrast is V-shaped in surface luminance with its minimum where the two are
// equal, so the worst reachable surface is the one closest to the foreground - which is an
// endpoint only when the foreground lies outside the range.
func worstContrast(fg rgb, minL, maxL float64) float64 {
	l := fg.relativeLuminance()
	worst := math.Max(minL, math.Min(maxL, l))
	return luminanceContrast(l, worst)
}

func luminanceContrast(a, b float64) float64 {
	if a < b {
		a, b = b, a
	}
	return (a + 0.05) / (b + 0.05)
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
var surfaceTwoToken = regexp.MustCompile(`--v-surface-2:\s*rgb\(var\(--v-veil-tint\)\s*/\s*([0-9.]+)\)`)

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

func surfaceTwoAlpha(t *testing.T, body string) float64 {
	t.Helper()
	m := surfaceTwoToken.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("--v-surface-2 is not a tint-over-backdrop value; the nested-surface proof no longer applies")
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
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

// backdropUrl pulls the bundled file out of `--v-backdrop-image: url("./backdrops/x.jpg");`. It
// deliberately does not accept the configured-background route: there is no file to measure there,
// which is the whole reason that case has its own, heavier scrim.
var backdropUrl = regexp.MustCompile(`--v-backdrop-image:\s*url\("\./([A-Za-z0-9._/-]+)"\)\s*;`)

func backdropFile(t *testing.T, body string) string {
	t.Helper()
	m := backdropUrl.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no bundled --v-backdrop-image in this block")
	}
	return m[1]
}

func decodeBackdrop(t *testing.T, file string) [][3]float64 {
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
	px := make([][3]float64, 0, b.Dx()*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			px = append(px, [3]float64{float64(r >> 8), float64(g >> 8), float64(bl >> 8)})
		}
	}
	return px
}

// backdropChannelBox is the per-channel minimum and maximum over the whole file - the bound that
// survives backdrop-filter, since a blurred sample mixes each channel independently.
func backdropChannelBox(t *testing.T, file string) (lo, hi rgb) {
	t.Helper()
	lo, hi = rgb{r: 255, g: 255, b: 255}, rgb{}
	for _, p := range decodeBackdrop(t, file) {
		lo = rgb{math.Min(lo.r, p[0]), math.Min(lo.g, p[1]), math.Min(lo.b, p[2])}
		hi = rgb{math.Max(hi.r, p[0]), math.Max(hi.g, p[1]), math.Max(hi.b, p[2])}
	}
	return lo, hi
}

func backdropSaturation(t *testing.T, file string) float64 {
	t.Helper()
	px := decodeBackdrop(t, file)
	var total float64
	for _, p := range px {
		high := math.Max(p[0], math.Max(p[1], p[2]))
		if high > 0 {
			total += (high - math.Min(p[0], math.Min(p[1], p[2]))) / high
		}
	}
	return total / float64(len(px))
}
