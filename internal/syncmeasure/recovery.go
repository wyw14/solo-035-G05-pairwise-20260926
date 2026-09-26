package syncmeasure

import (
	"fmt"

	"github.com/wyw14/solo-035/internal/model"
)

// RecoveryRecord persists measured marker/pulse pairs for one event so a
// restarted playout service retains the evidence behind its proofs.
type RecoveryRecord struct {
	EventID      model.EventID         `json:"event_id"`
	Measurements []RecordedMeasurement `json:"measurements"`
}

func (archive *Archive) Export(eventIDs []model.EventID) []RecoveryRecord {
	records := make([]RecoveryRecord, 0, len(eventIDs))
	for _, eventID := range eventIDs {
		items := archive.List(eventID)
		if len(items) == 0 {
			continue
		}
		records = append(records, RecoveryRecord{EventID: eventID, Measurements: items})
	}
	return records
}

func (archive *Archive) ExportAll() []RecoveryRecord {
	archive.mu.RLock()
	eventIDs := make([]model.EventID, 0, len(archive.byEvent))
	for eventID := range archive.byEvent {
		eventIDs = append(eventIDs, eventID)
	}
	archive.mu.RUnlock()
	return archive.Export(eventIDs)
}

func (archive *Archive) Restore(records []RecoveryRecord) error {
	for _, record := range records {
		if !record.EventID.Valid() {
			return fmt.Errorf("sync measurement recovery record has invalid event")
		}
		if err := archive.Replace(record.EventID, record.Measurements); err != nil {
			return err
		}
	}
	return nil
}
