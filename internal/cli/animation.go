package cli

import (
	"time"

	"github.com/charmbracelet/harmonica"
)

const animationFrameTime = 32 * time.Millisecond

// springFrames samples a critically damped spring over one animation.
// Keep the endpoints exact so the resting frame and the skipped frame match.
func springFrames(last int) []float64 {
	frames := make([]float64, last+1)
	spring := harmonica.NewSpring(1/float64(last), 6, 1)
	position, velocity := 0.0, 0.0
	for i := 1; i < last; i++ {
		position, velocity = spring.Update(position, velocity, 1)
		frames[i] = position
	}
	frames[last] = 1
	return frames
}
