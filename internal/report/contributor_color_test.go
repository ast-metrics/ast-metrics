package report

import (
	"math"
	"strconv"
	"testing"
)

// relativeLuminance follows the WCAG 2.x definition for an sRGB "#rrggbb" color.
func relativeLuminance(t *testing.T, hex string) float64 {
	t.Helper()
	v, err := strconv.ParseUint(hex[1:], 16, 32)
	if len(hex) != 7 || hex[0] != '#' || err != nil {
		t.Fatalf("invalid color %q", hex)
	}
	channels := []uint64{v >> 16 & 0xff, v >> 8 & 0xff, v & 0xff}
	weights := []float64{0.2126, 0.7152, 0.0722}
	luminance := 0.0
	for i, c := range channels {
		s := float64(c) / 255
		if s <= 0.03928 {
			s = s / 12.92
		} else {
			s = math.Pow((s+0.055)/1.055, 2.4)
		}
		luminance += weights[i] * s
	}
	return luminance
}

// The Lighthouse job asserts a perfect accessibility score, and which color a
// contributor gets depends on the hash of their name. Checking the palette
// itself keeps the report accessible whoever authored the analyzed commits.
func TestContributorColorsContrastWithWhiteInitials(t *testing.T) {
	colors := append([]string{contributorFallbackColor}, contributorPalette...)
	for _, color := range colors {
		ratio := 1.05 / (relativeLuminance(t, color) + 0.05)
		if ratio < 4.5 {
			t.Errorf("%s has a contrast of %.2f:1 against white, want at least 4.5:1", color, ratio)
		}
	}
}
