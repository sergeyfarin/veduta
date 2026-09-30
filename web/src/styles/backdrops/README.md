# Bundled Veil backgrounds

- Light: Luigi Querena, *Campo di San Giovanni e Paolo, Venice*; 1600x1112.
- Dark: Joseph Vernet, *Entrance to the Port of Palermo by Moonlight*; existing asset retained.

The light reproduction was selected by the user on 2026-09-30:
[original image](https://blogger.googleusercontent.com/img/b/R29vZ2xl/AVvXsEgORNKDN55oVEu_dekL-x7fyowVfFGRnrAJU1I0P2PrPmPjGyIoiM-2jblgJZFwvkHLhDH8QO23yC2f0LdDakLWKgXQO_pelrg683GvUv5tFIRubRH8xMfeUGqZv22xd7NdpF9B38efW_E/s1600/Luigi-Querena-Campo-di-San-Giovanni-e-Paolo-Venice.jpg). Its architectural composition is preserved; no generated
content is used. The previous Canaletto asset is no longer bundled.

For the light JPEG, decode the source to 8-bit RGB and compute
`gray = 0.299*r + 0.587*g + 0.114*b`. For each channel, encode
`round(0.4 * (gray + 0.25 * (channel - gray)) + 0.6 * tint)`, where
`tint` is `(232, 238, 246)`, then save at JPEG quality 90 without resizing.
This reduces saturation and contrast while preserving the painting's details.

Both backgrounds use centered `cover` sizing on the full page, including tall portrait and
wide landscape screens. Extreme aspect ratios crop the painting. The shipped pixels are checked
by `TestBundledBackdropTone`, `TestVeilContrast`, and
`TestVeilFallbackGradientIsInsideTheBackdrop`.
