package service

import (
	"fmt"

	"github.com/wyw14/solo-035/internal/backup"
	"github.com/wyw14/solo-035/internal/event"
	"github.com/wyw14/solo-035/internal/journal"
	"github.com/wyw14/solo-035/internal/model"
	"github.com/wyw14/solo-035/internal/quality"
	"github.com/wyw14/solo-035/internal/syncer"
	"github.com/wyw14/solo-035/internal/syncmeasure"
)

type ControlCheckpoint struct {
	Events          []event.RecoveryRecord       `json:"events"`
	Sync            []syncer.Recovery            `json:"sync"`
	SyncMeasurement []syncmeasure.RecoveryRecord `json:"sync_measurements"`
	Backup          []backup.RecoveryRecord      `json:"backup"`
	Quality         []quality.RecoveryRecord     `json:"quality"`
	Journal         []journal.Record             `json:"journal"`
}

func (runtime *Runtime) ExportCheckpoint() ControlCheckpoint {
	events := runtime.Events.ExportRecovery()
	ids := make([]model.EventID, 0, len(events))
	for _, record := range events {
		ids = append(ids, record.Event.ID)
	}
	return ControlCheckpoint{
		Events:          events,
		Sync:            runtime.SyncState.ExportRecovery(),
		SyncMeasurement: runtime.SyncMeasurements.Export(ids),
		Backup:          runtime.BackupState.ExportRecovery(),
		Quality:         runtime.Evidence.Export(ids),
		Journal:         runtime.Journal.Records(),
	}
}

func (runtime *Runtime) RestoreCheckpoint(checkpoint ControlCheckpoint) (RecoveryReport, error) {
	if err := runtime.Events.Restore(checkpoint.Events, runtime.Clock()); err != nil {
		return RecoveryReport{}, fmt.Errorf("restore events: %w", err)
	}
	if err := runtime.SyncState.RestoreRecovery(checkpoint.Sync); err != nil {
		return RecoveryReport{}, fmt.Errorf("restore synchronization: %w", err)
	}
	if err := runtime.SyncMeasurements.Restore(checkpoint.SyncMeasurement); err != nil {
		return RecoveryReport{}, fmt.Errorf("restore sync measurements: %w", err)
	}
	if err := runtime.BackupState.RestoreRecovery(checkpoint.Backup); err != nil {
		return RecoveryReport{}, fmt.Errorf("restore failovers: %w", err)
	}
	if err := runtime.Evidence.Restore(checkpoint.Quality); err != nil {
		return RecoveryReport{}, fmt.Errorf("restore quality evidence: %w", err)
	}
	sources := make(map[model.EventID]model.SourceID)
	for _, item := range checkpoint.Sync {
		sources[item.EventID] = item.Source
	}
	return runtime.RestoreFromRecords(checkpoint.Journal, func(eventID model.EventID) model.SourceID {
		sourceID := sources[eventID]
		if sourceID == "" {
			sourceID = model.NewSourceID()
		}
		return sourceID
	})
}
