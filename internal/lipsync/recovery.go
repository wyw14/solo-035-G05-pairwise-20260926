package lipsync

import (
	"fmt"
	"sort"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

type RecoveryObservation struct {
	Index      int64     `json:"index"`
	ObservedAt time.Time `json:"observed_at"`
}

type RecoveryRecord struct {
	EventID          model.EventID         `json:"event_id"`
	Source           model.SourceID        `json:"source"`
	SourceGeneration uint64                `json:"source_generation"`
	Revision         model.RevisionID      `json:"revision"`
	Markers          []RecoveryObservation `json:"markers"`
	Pulses           []RecoveryObservation `json:"pulses"`
}

func (archive *Archive) Export() []RecoveryRecord {
	archive.mu.RLock()
	defer archive.mu.RUnlock()
	records := make([]RecoveryRecord, 0, len(archive.accumulators))
	for eventID, acc := range archive.accumulators {
		record := RecoveryRecord{
			EventID:          eventID,
			Source:           acc.source,
			SourceGeneration: acc.generation,
			Revision:         acc.revision,
			Markers:          make([]RecoveryObservation, 0, len(acc.markers)),
			Pulses:           make([]RecoveryObservation, 0, len(acc.pulses)),
		}
		for index, at := range acc.markers {
			record.Markers = append(record.Markers, RecoveryObservation{Index: index, ObservedAt: at})
		}
		for index, at := range acc.pulses {
			record.Pulses = append(record.Pulses, RecoveryObservation{Index: index, ObservedAt: at})
		}
		sort.Slice(record.Markers, func(i, j int) bool { return record.Markers[i].Index < record.Markers[j].Index })
		sort.Slice(record.Pulses, func(i, j int) bool { return record.Pulses[i].Index < record.Pulses[j].Index })
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool { return records[i].EventID < records[j].EventID })
	return records
}

func (archive *Archive) Restore(records []RecoveryRecord) error {
	archive.mu.Lock()
	archive.accumulators = make(map[model.EventID]*accumulator)
	archive.mu.Unlock()
	for _, record := range records {
		if err := validateIdentity(record.EventID, record.Source, record.SourceGeneration, record.Revision); err != nil {
			return fmt.Errorf("lip-sync recovery record is invalid: %w", err)
		}
		// 按采集时刻交错回放标记与脉冲，使恢复后的配对与统计和原始顺序一致。
		observations := make([]typedObservation, 0, len(record.Markers)+len(record.Pulses))
		for _, marker := range record.Markers {
			observations = append(observations, typedObservation{at: marker.ObservedAt, index: marker.Index, pulse: false})
		}
		for _, pulse := range record.Pulses {
			observations = append(observations, typedObservation{at: pulse.ObservedAt, index: pulse.Index, pulse: true})
		}
		sort.SliceStable(observations, func(i, j int) bool { return observations[i].at.Before(observations[j].at) })
		for _, observation := range observations {
			if observation.pulse {
				if err := archive.RecordPulse(record.EventID, model.AudioPulse{
					Source:           record.Source,
					SourceGeneration: record.SourceGeneration,
					Revision:         record.Revision,
					Index:            observation.index,
					ObservedAt:       observation.at,
				}); err != nil {
					return err
				}
				continue
			}
			if err := archive.RecordMarker(record.EventID, model.SyncMarker{
				Source:           record.Source,
				SourceGeneration: record.SourceGeneration,
				Revision:         record.Revision,
				Index:            observation.index,
				ObservedAt:       observation.at,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

type typedObservation struct {
	at    time.Time
	index int64
	pulse bool
}
