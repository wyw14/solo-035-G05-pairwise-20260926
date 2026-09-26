package syncmeasure

import (
	"fmt"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

// Measurement is one matched pair of a video marker and an audio pulse.
// Offset is derived from the two observed timestamps; positive offset means
// audio arrives after video.
type Measurement struct {
	Source           model.SourceID
	SourceGeneration uint64
	Revision         model.RevisionID
	MarkerID         string
	VideoAt          time.Time
	AudioAt          time.Time
	RecordedAt       time.Time
}

// RecordedMeasurement is an archived measurement carrying the generation it
// was observed under. Older generations are retained so the archive can show
// why they can no longer prove the current generation.
type RecordedMeasurement struct {
	Source           model.SourceID   `json:"source"`
	SourceGeneration uint64           `json:"source_generation"`
	Revision         model.RevisionID `json:"revision"`
	MarkerID         string           `json:"marker_id"`
	OffsetMs         float64          `json:"offset_ms"`
	VideoAt          time.Time        `json:"video_at"`
	AudioAt          time.Time        `json:"audio_at"`
	RecordedAt       time.Time        `json:"recorded_at"`
}

func (measurement Measurement) validate() error {
	if !measurement.Source.Valid() {
		return fmt.Errorf("sync measurement lacks a valid signal source")
	}
	if measurement.SourceGeneration == 0 {
		return fmt.Errorf("sync measurement lacks the source generation")
	}
	if !measurement.Revision.Valid() {
		return fmt.Errorf("sync measurement lacks the program revision")
	}
	if measurement.MarkerID == "" {
		return fmt.Errorf("sync measurement lacks marker identity")
	}
	if measurement.VideoAt.IsZero() || measurement.AudioAt.IsZero() {
		return fmt.Errorf("sync measurement requires video and audio timestamps")
	}
	return nil
}

func (measurement Measurement) record() (RecordedMeasurement, error) {
	if err := measurement.validate(); err != nil {
		return RecordedMeasurement{}, err
	}
	recorded := RecordedMeasurement{
		Source:           measurement.Source,
		SourceGeneration: measurement.SourceGeneration,
		Revision:         measurement.Revision,
		MarkerID:         measurement.MarkerID,
		OffsetMs:         float64(measurement.AudioAt.Sub(measurement.VideoAt)) / float64(time.Millisecond),
		VideoAt:          measurement.VideoAt.UTC(),
		AudioAt:          measurement.AudioAt.UTC(),
		RecordedAt:       measurement.RecordedAt.UTC(),
	}
	return recorded, nil
}
