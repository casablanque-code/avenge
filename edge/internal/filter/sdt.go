// Package filter implements the Swinging Door Trending (SDT) algorithm
// for lossless-within-deadband compression of time-series data.
//
// Reference: E.H. Bristol, "Swinging door trending: Adaptive trend
// recording", ISA National Conference Proceedings, 1990.
//
// The algorithm maintains two "doors" (upper and lower slope limits)
// anchored at the last stored point. A new point is kept only when it
// falls outside both doors, i.e. when the trend has genuinely changed
// beyond the deadband ε.
package filter

import "math"

// Point is a (time, value) pair.
type Point struct {
	T     float64
	Value float64
}

// SDT compresses a slice of points using the Swinging Door Trending
// algorithm with deadband epsilon.
//
// Returns the subset of input points that are "turning points" —
// the minimal representation that reconstructs the signal within ±epsilon.
func SDT(points []Point, epsilon float64) []Point {
	if len(points) == 0 {
		return nil
	}
	if len(points) == 1 {
		return []Point{points[0]}
	}

	result := []Point{points[0]}
	anchor := points[0]

	// Slope limits for the two doors.
	slopeHigh := math.Inf(+1)
	slopeLow := math.Inf(-1)

	for i := 1; i < len(points); i++ {
		p := points[i]
		dt := p.T - anchor.T
		if dt <= 0 {
			continue
		}

		// Slopes from anchor to upper/lower edge of this point's deadband.
		sh := (p.Value + epsilon - anchor.Value) / dt
		sl := (p.Value - epsilon - anchor.Value) / dt

		// Narrow the doors.
		if sh < slopeHigh {
			slopeHigh = sh
		}
		if sl > slopeLow {
			slopeLow = sl
		}

		// Doors have crossed — this point cannot be represented within
		// the current trend. Store the previous point and reset.
		if slopeLow > slopeHigh {
			prev := points[i-1]
			result = append(result, prev)
			anchor = prev
			dt2 := p.T - anchor.T
			if dt2 <= 0 {
				continue
			}
			slopeHigh = (p.Value + epsilon - anchor.Value) / dt2
			slopeLow = (p.Value - epsilon - anchor.Value) / dt2
		}
	}

	// Always include the last point.
	last := points[len(points)-1]
	if result[len(result)-1] != last {
		result = append(result, last)
	}
	return result
}

// CompressionRatio returns len(original) / len(compressed).
// Returns 1.0 for empty or single-element input.
func CompressionRatio(original, compressed int) float64 {
	if compressed == 0 {
		return 1
	}
	return float64(original) / float64(compressed)
}
