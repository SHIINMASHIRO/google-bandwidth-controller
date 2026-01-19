package bandwidth

import (
	"math"
	"math/rand"
)

// Clamp restricts a value to be within a specified range
func Clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// Clamp64 restricts an int64 value to be within a specified range
func Clamp64(value, min, max int64) int64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ClampFloat restricts a float64 value to be within a specified range
func ClampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// Min64 returns the minimum of two int64 values
func Min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// Max64 returns the maximum of two int64 values
func Max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// CalculateConcurrency calculates the number of concurrent servers using sine waves
func CalculateConcurrency(elapsedSeconds float64, min, max int, randomness float64) int {
	// Multiple overlapping sine waves for complex pattern
	wave1 := math.Sin(elapsedSeconds / 300) // 5-minute cycle
	wave2 := math.Sin(elapsedSeconds / 180) // 3-minute cycle
	wave3 := math.Sin(elapsedSeconds / 420) // 7-minute cycle

	combined := (wave1 + wave2*0.5 + wave3*0.3) / 1.8

	// Map to concurrent range (0 to 1)
	normalized := (combined + 1) / 2

	// Calculate base concurrency
	concurrent := min + int(float64(max-min)*normalized)

	// Add randomness
	randomAdjust := rand.Intn(3) - 1 // -1, 0, or 1
	concurrent += randomAdjust

	return Clamp(concurrent, min, max)
}

// WeightedRandomSelection selects N items from a list using weighted random selection
func WeightedRandomSelection(count int, weights []float64) []int {
	if count >= len(weights) {
		// Return all indices
		result := make([]int, len(weights))
		for i := range result {
			result[i] = i
		}
		return result
	}

	selected := make([]int, 0, count)
	available := make([]int, len(weights))
	availableWeights := make([]float64, len(weights))
	copy(availableWeights, weights)

	for i := range available {
		available[i] = i
	}

	for i := 0; i < count; i++ {
		// Calculate total weight
		totalWeight := 0.0
		for _, w := range availableWeights {
			totalWeight += w
		}

		if totalWeight == 0 {
			// All weights are 0, select randomly
			idx := rand.Intn(len(available))
			selected = append(selected, available[idx])

			// Remove from available
			available = append(available[:idx], available[idx+1:]...)
			availableWeights = append(availableWeights[:idx], availableWeights[idx+1:]...)
			continue
		}

		// Select based on weighted probability
		r := rand.Float64() * totalWeight
		cumulative := 0.0

		selectedIdx := -1
		for idx, w := range availableWeights {
			cumulative += w
			if r <= cumulative {
				selectedIdx = idx
				break
			}
		}

		if selectedIdx == -1 {
			selectedIdx = len(available) - 1
		}

		selected = append(selected, available[selectedIdx])

		// Remove from available
		available = append(available[:selectedIdx], available[selectedIdx+1:]...)
		availableWeights = append(availableWeights[:selectedIdx], availableWeights[selectedIdx+1:]...)
	}

	return selected
}

// AllocateBandwidth allocates bandwidth across N agents with variance
func AllocateBandwidth(targetTotal float64, numAgents int, minPerAgent, maxPerAgent int64, randomnessFactor float64) []int64 {
	if numAgents == 0 {
		return []int64{}
	}

	allocations := make([]int64, numAgents)

	// Generate random weights
	weights := make([]float64, numAgents)
	totalWeight := 0.0

	for i := 0; i < numAgents; i++ {
		// Base weight with randomness (0.5 to 1.5)
		weight := 0.5 + rand.Float64()
		weights[i] = weight
		totalWeight += weight
	}

	// Allocate bandwidth proportionally
	actualTotal := int64(0)
	for i := 0; i < numAgents; i++ {
		proportion := weights[i] / totalWeight
		allocated := int64(proportion * targetTotal)

		// Apply constraints
		allocated = Clamp64(allocated, minPerAgent, maxPerAgent)

		// Add small random variation
		variation := float64(allocated) * randomnessFactor * (rand.Float64()*2 - 1)
		allocated = int64(float64(allocated) + variation)
		allocated = Clamp64(allocated, minPerAgent, maxPerAgent)

		allocations[i] = allocated
		actualTotal += allocated
	}

	// Normalize to hit target exactly
	if actualTotal != int64(targetTotal) && actualTotal > 0 {
		ratio := targetTotal / float64(actualTotal)
		for i := 0; i < numAgents; i++ {
			allocations[i] = int64(float64(allocations[i]) * ratio)
			allocations[i] = Clamp64(allocations[i], minPerAgent, maxPerAgent)
		}
	}

	return allocations
}

// CalculateStagger calculates staggered delays for smooth transitions
func CalculateStagger(count int, totalDuration float64) []float64 {
	if count == 0 {
		return []float64{}
	}

	delays := make([]float64, count)
	baseDelay := totalDuration / float64(count)

	for i := 0; i < count; i++ {
		delay := float64(i) * baseDelay

		// Add jitter (±25% of base delay)
		jitter := baseDelay * 0.25 * (rand.Float64()*2 - 1)
		delay += jitter

		if delay < 0 {
			delay = 0
		}

		delays[i] = delay
	}

	return delays
}

// RandomDuration generates a random duration within a range with additional jitter
func RandomDuration(minSeconds, maxSeconds float64, jitterFactor float64) float64 {
	// Random within range
	duration := minSeconds + rand.Float64()*(maxSeconds-minSeconds)

	// Add jitter
	jitter := duration * jitterFactor * (rand.Float64()*2 - 1)
	duration += jitter

	return ClampFloat(duration, minSeconds, maxSeconds)
}
