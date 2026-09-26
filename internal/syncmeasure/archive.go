package syncmeasure

import (
	"fmt"
	"sort"
	"sync"

	"github.com/wyw14/solo-035/internal/model"
)

// Archive retains every measured marker/pulse pair per event. Measurements of
// superseded generations are kept on purpose: the profile must show why they
// cannot prove the generation currently on air.
type Archive struct {
	mu      sync.RWMutex
	byEvent map[model.EventID][]RecordedMeasurement
}

func NewArchive() *Archive {
	return &Archive{byEvent: make(map[model.EventID][]RecordedMeasurement)}
}

func (archive *Archive) Add(eventID model.EventID, measurement Measurement) (RecordedMeasurement, error) {
	if !eventID.Valid() {
		return RecordedMeasurement{}, fmt.Errorf("sync measurement lacks event identity")
	}
	recorded, err := measurement.record()
	if err != nil {
		return RecordedMeasurement{}, err
	}
	archive.mu.Lock()
	defer archive.mu.Unlock()
	for _, existing := range archive.byEvent[eventID] {
		if existing.MarkerID == recorded.MarkerID && existing.SourceGeneration == recorded.SourceGeneration {
			return RecordedMeasurement{}, fmt.Errorf("sync marker %s was already recorded for source generation %d", recorded.MarkerID, recorded.SourceGeneration)
		}
	}
	archive.byEvent[eventID] = append(archive.byEvent[eventID], recorded)
	return recorded, nil
}

func (archive *Archive) List(eventID model.EventID) []RecordedMeasurement {
	archive.mu.RLock()
	defer archive.mu.RUnlock()
	items := append([]RecordedMeasurement(nil), archive.byEvent[eventID]...)
	sort.Slice(items, func(i, j int) bool {
		if !items[i].RecordedAt.Equal(items[j].RecordedAt) {
			return items[i].RecordedAt.Before(items[j].RecordedAt)
		}
		return items[i].MarkerID < items[j].MarkerID
	})
	return items
}

func (archive *Archive) Replace(eventID model.EventID, items []RecordedMeasurement) error {
	if !eventID.Valid() {
		return fmt.Errorf("sync archive record has invalid event")
	}
	seen := make(map[string]bool)
	copied := make([]RecordedMeasurement, 0, len(items))
	for _, item := range items {
		measurement := Measurement{
			Source:           item.Source,
			SourceGeneration: item.SourceGeneration,
			Revision:         item.Revision,
			MarkerID:         item.MarkerID,
			VideoAt:          item.VideoAt,
			AudioAt:          item.AudioAt,
			RecordedAt:       item.RecordedAt,
		}
		recorded, err := measurement.record()
		if err != nil {
			return err
		}
		key := fmt.Sprintf("%s@%d", recorded.MarkerID, recorded.SourceGeneration)
		if seen[key] {
			return fmt.Errorf("sync archive for event %s contains duplicate marker %s", eventID, recorded.MarkerID)
		}
		seen[key] = true
		copied = append(copied, recorded)
	}
	archive.mu.Lock()
	defer archive.mu.Unlock()
	archive.byEvent[eventID] = copied
	return nil
}

func (archive *Archive) EventCount() int {
	archive.mu.RLock()
	defer archive.mu.RUnlock()
	return len(archive.byEvent)
}
