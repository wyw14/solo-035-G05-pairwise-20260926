package syncmeasure

import (
	"fmt"
	"sort"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

// Tolerance configures when measured offsets count as a stable interval.
type Tolerance struct {
	// RequiredSamples is the minimum number of samples the trailing stable
	// interval must contain.
	RequiredSamples int
	// JitterMillis bounds the spread (max-min) inside the stable interval.
	JitterMillis float64
	// MaxSampleAge flags the generation as stale when the newest measurement
	// is older than this.
	MaxSampleAge time.Duration
}

func (tolerance Tolerance) validate() error {
	if tolerance.RequiredSamples < 2 {
		return fmt.Errorf("sync measurement requires at least two samples")
	}
	if tolerance.JitterMillis <= 0 {
		return fmt.Errorf("sync jitter tolerance must be positive")
	}
	if tolerance.MaxSampleAge <= 0 {
		return fmt.Errorf("sync sample age must be positive")
	}
	return nil
}

// Group identifies one signal source feeding one program revision.
type Group struct {
	EventID  model.EventID    `json:"event_id"`
	Source   model.SourceID   `json:"source"`
	Revision model.RevisionID `json:"revision"`
}

// Estimate is the offset and stable interval derived from real measurements
// of a single source generation.
type Estimate struct {
	Group Group `json:"group"`
	// Generation is the source generation the measurements belong to.
	Generation uint64 `json:"source_generation"`
	// Status is stable, jitter, missing or stale.
	Status string `json:"status"`
	// Reason explains missing measurements, stale evidence or jitter.
	Reason string `json:"reason"`
	// SampleCount is the number of measurements for this generation.
	SampleCount int `json:"sample_count"`
	// StableCount is how many trailing samples fit inside the stable interval.
	StableCount int `json:"stable_count"`
	// OffsetMillis is the mean offset over the stable interval. It is nil when
	// no interval can be established, never a fabricated constant.
	OffsetMillis *float64 `json:"offset_ms,omitempty"`
	// LowerMillis/UpperMillis bound the stable interval.
	LowerMillis *float64 `json:"lower_ms,omitempty"`
	UpperMillis *float64 `json:"upper_ms,omitempty"`
	// JitterMillis is the spread of the stable interval.
	JitterMillis *float64 `json:"jitter_ms,omitempty"`
	// TotalSpreadMillis is the spread over every sample, including outliers.
	TotalSpreadMillis *float64 `json:"total_spread_ms,omitempty"`
	// FirstMeasuredAt/LastMeasuredAt bracket the observed samples.
	FirstMeasuredAt *time.Time `json:"first_measured_at,omitempty"`
	LastMeasuredAt  *time.Time `json:"last_measured_at,omitempty"`
	// UpdatedAt is when the estimate was last computed.
	UpdatedAt time.Time `json:"updated_at"`
}

const (
	StatusStable  = "stable"
	StatusJitter  = "jitter"
	StatusMissing = "missing"
	StatusStale   = "stale"
)

// estimate derives the offset and stable interval from measurements that all
// belong to the same source/revision/generation triple. Callers decide
// whether that generation is still eligible as proof.
func estimate(group Group, generation uint64, items []RecordedMeasurement, tolerance Tolerance, now time.Time) Estimate {
	result := Estimate{Group: group, Generation: generation, Status: StatusMissing, UpdatedAt: now.UTC()}

	sort.Slice(items, func(i, j int) bool {
		if !items[i].RecordedAt.Equal(items[j].RecordedAt) {
			return items[i].RecordedAt.Before(items[j].RecordedAt)
		}
		return items[i].MarkerID < items[j].MarkerID
	})
	result.SampleCount = len(items)
	if len(items) == 0 {
		result.Reason = fmt.Sprintf("no video-marker/audio-pulse measurement for source generation %d", generation)
		return result
	}

	first := items[0].RecordedAt.UTC()
	last := items[len(items)-1].RecordedAt.UTC()
	result.FirstMeasuredAt = &first
	result.LastMeasuredAt = &last

	stale := false
	if age := now.Sub(last); age > tolerance.MaxSampleAge {
		stale = true
		result.Status = StatusStale
		result.Reason = fmt.Sprintf("newest measurement is %s old, exceeding the %s evidence window", age.Round(time.Millisecond), tolerance.MaxSampleAge)
	}

	offsets := make([]float64, len(items))
	for i, item := range items {
		offsets[i] = item.OffsetMs
	}
	minOffset, maxOffset := offsets[0], offsets[0]
	for _, offset := range offsets[1:] {
		minOffset = minFloat(minOffset, offset)
		maxOffset = maxFloat(maxOffset, offset)
	}
	spread := maxOffset - minOffset
	result.TotalSpreadMillis = &spread

	// Longest trailing run whose spread stays inside the jitter tolerance.
	start := len(offsets) - 1
	runMin, runMax := offsets[start], offsets[start]
	for start > 0 {
		next := offsets[start-1]
		nextMin, nextMax := minFloat(runMin, next), maxFloat(runMax, next)
		if nextMax-nextMin > tolerance.JitterMillis {
			break
		}
		runMin, runMax = nextMin, nextMax
		start--
	}
	stableOffsets := offsets[start:]
	result.StableCount = len(stableOffsets)

	if result.StableCount < tolerance.RequiredSamples {
		if !stale {
			result.Status = StatusJitter
			result.Reason = fmt.Sprintf("only %d of %d required sample(s) stay within %.1f ms spread; observed spread %.1f ms", result.StableCount, tolerance.RequiredSamples, tolerance.JitterMillis, spread)
		}
		return result
	}

	mean := 0.0
	for _, offset := range stableOffsets {
		mean += offset
	}
	mean /= float64(len(stableOffsets))
	jitter := runMax - runMin
	result.OffsetMillis = &mean
	result.LowerMillis = &runMin
	result.UpperMillis = &runMax
	result.JitterMillis = &jitter
	if stale {
		return result
	}
	result.Status = StatusStable
	result.Reason = fmt.Sprintf("%d samples within %.1f ms spread", result.StableCount, jitter)
	return result
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
