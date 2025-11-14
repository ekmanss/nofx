package trailingstop

import "math"

const floatEqualityEpsilon = 1e-6

func floatsAlmostEqual(a, b float64) bool {
	return math.Abs(a-b) <= floatEqualityEpsilon
}
