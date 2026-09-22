package cache

import "math"

// Thin aliases so the cache code reads cleanly; these are the standard IEEE-754
// bit-cast helpers used for the key hash and the lock-free float accumulator.
func float64bits(f float64) uint64     { return math.Float64bits(f) }
func float64frombits(b uint64) float64 { return math.Float64frombits(b) }
