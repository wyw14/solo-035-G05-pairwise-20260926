package service

import (
	"context"
	"fmt"
	"time"

	"github.com/wyw14/solo-035/internal/caption"
	"github.com/wyw14/solo-035/internal/model"
)

type Coordinator struct {
	runtime *Runtime
}

func NewCoordinator(runtime *Runtime) *Coordinator {
	return &Coordinator{runtime: runtime}
}

func (coordinator *Coordinator) CreateEvent(name string) (model.LiveEvent, error) {
	event, err := coordinator.runtime.Events.Create(name, coordinator.runtime.Clock())
	if err != nil {
		return model.LiveEvent{}, err
	}
	if _, err := coordinator.runtime.EventFlow.Arm(event.ID); err != nil {
		return model.LiveEvent{}, err
	}
	return coordinator.runtime.Events.GetOrZero(event.ID), nil
}

func (coordinator *Coordinator) PrepareSignal(eventID model.EventID, sourceID model.SourceID) (model.SignalStatus, error) {
	event, ok := coordinator.runtime.Events.Get(eventID)
	if !ok {
		return model.SignalStatus{}, fmt.Errorf("event %s not found", eventID)
	}
	status := coordinator.runtime.SourceFlow.Switch(sourceID)
	event = event.NewSource(coordinator.runtime.Clock())
	event.SourceGeneration = status.Generation
	if err := coordinator.runtime.Events.Update(event); err != nil {
		return model.SignalStatus{}, err
	}
	coordinator.runtime.Quality.Reset(eventID)
	coordinator.runtime.LipSync.Reset(eventID)
	if err := coordinator.runtime.Sync.Open(eventID, 3); err != nil {
		return model.SignalStatus{}, err
	}
	return status, nil
}

func (coordinator *Coordinator) ConfirmSignal(sourceID model.SourceID, command model.CommandID) error {
	if err := coordinator.runtime.SourceFlow.Confirm(sourceID, command, true, true); err != nil {
		return err
	}
	if !coordinator.runtime.SourceFlow.Ready(sourceID) {
		return fmt.Errorf("source %s did not retain its current lock", sourceID)
	}
	return nil
}

func (coordinator *Coordinator) PushFrame(eventID model.EventID, frame model.Frame) error {
	received, err := coordinator.runtime.Receiver.Push(frame.Source, frame.SourceGeneration, frame.Sequence, frame.Timecode, frame.CapturedAt)
	if err != nil {
		return err
	}
	received.VideoLocked = frame.VideoLocked
	received.AudioLocked = frame.AudioLocked
	_, err = coordinator.runtime.Sync.Observe(eventID, received)
	return err
}

func (coordinator *Coordinator) ConfigureProgram(eventID model.EventID, sourceID model.SourceID) error {
	event, ok := coordinator.runtime.Events.Get(eventID)
	if !ok {
		return fmt.Errorf("event %s not found", eventID)
	}
	lease, err := coordinator.runtime.EncoderFlow.ReservePrimary(eventID, "1080p50")
	if err != nil {
		return err
	}
	if err := coordinator.runtime.EncoderFlow.Start(lease); err != nil {
		return err
	}
	program, err := coordinator.runtime.Mux.Configure(event, lease, 8000)
	if err != nil {
		return err
	}
	if err := coordinator.runtime.Mux.Activate(eventID, program.Revision); err != nil {
		return err
	}
	return coordinator.runtime.Playout.Stage(event, sourceID, lease.Fence, "current-program")
}

func (coordinator *Coordinator) EstablishInterlocks(eventID model.EventID) error {
	event, ok := coordinator.runtime.Events.Get(eventID)
	if !ok {
		return fmt.Errorf("event %s not found", eventID)
	}
	waveA, _, err := coordinator.runtime.AudioMonitor.Measure("probe-a", -18, coordinator.runtime.Clock())
	if err != nil {
		return err
	}
	waveB, _, err := coordinator.runtime.AudioMonitor.Measure("probe-b", -17, coordinator.runtime.Clock())
	if err != nil {
		return err
	}
	if err := coordinator.runtime.Evaluator.Accept(waveA); err != nil {
		return err
	}
	if err := coordinator.runtime.Evaluator.Accept(waveB); err != nil {
		return err
	}
	if err := coordinator.runtime.Quality.Record(eventID, waveA); err != nil {
		return err
	}
	if err := coordinator.runtime.Quality.Record(eventID, waveB); err != nil {
		return err
	}
	if err := coordinator.runtime.Continuity.Accept(eventID, caption.Cue{Revision: event.Revision, Sequence: 1, Text: "ON AIR"}); err != nil {
		return err
	}
	coordinator.runtime.CaptionFlow.Encode(eventID, event.Revision, "ON AIR")
	return nil
}

func (coordinator *Coordinator) CommitOnAir(eventID model.EventID, fence uint64) error {
	event, ok := coordinator.runtime.Events.Get(eventID)
	if !ok {
		return fmt.Errorf("event %s not found", eventID)
	}
	if !coordinator.runtime.Sync.Ready(eventID) {
		return fmt.Errorf("frame synchronization is not stable")
	}
	if err := coordinator.runtime.CaptionFlow.Ready(eventID, event.Revision); err != nil {
		return err
	}
	if _, err := coordinator.runtime.Quality.Permit(eventID, true, true); err != nil {
		return err
	}
	output, ok := coordinator.runtime.Playout.Output(eventID)
	if !ok {
		return fmt.Errorf("playout output for event %s is not staged", eventID)
	}
	proof, _, err := coordinator.runtime.LipSync.Proof(eventID, output.Source, event.SourceGeneration, event.Revision, coordinator.runtime.Clock())
	if err != nil {
		// 缺测、抖动超限或属于旧代次/旧版本时，禁止用固定值冒充播出证明。
		return err
	}
	if err := coordinator.runtime.Playout.CommitOnAir(event, fence, proof); err != nil {
		return err
	}
	_, err = coordinator.runtime.EventFlow.MarkOnAir(eventID)
	return err
}

// RecordSyncMarker 记录视频同步标记的实测时间戳，按事件当前的信号源代次与节目版本归档。
func (coordinator *Coordinator) RecordSyncMarker(eventID model.EventID, sourceID model.SourceID, index int64, observedAt time.Time) (model.LipSyncProfile, error) {
	if err := coordinator.requireEvent(eventID); err != nil {
		return model.LipSyncProfile{}, err
	}
	if observedAt.IsZero() {
		observedAt = coordinator.runtime.Clock()
	}
	event, _ := coordinator.runtime.Events.Get(eventID)
	if err := coordinator.runtime.LipSync.RecordMarker(eventID, model.SyncMarker{
		Source:           sourceID,
		SourceGeneration: event.SourceGeneration,
		Revision:         event.Revision,
		Index:            index,
		ObservedAt:       observedAt,
	}); err != nil {
		return model.LipSyncProfile{}, err
	}
	return coordinator.LipSyncStatus(eventID), nil
}

// RecordAudioPulse 记录音频参考脉冲的实测时间戳，与同 index 的视频标记配对。
func (coordinator *Coordinator) RecordAudioPulse(eventID model.EventID, sourceID model.SourceID, index int64, observedAt time.Time) (model.LipSyncProfile, error) {
	if err := coordinator.requireEvent(eventID); err != nil {
		return model.LipSyncProfile{}, err
	}
	if observedAt.IsZero() {
		observedAt = coordinator.runtime.Clock()
	}
	event, _ := coordinator.runtime.Events.Get(eventID)
	if err := coordinator.runtime.LipSync.RecordPulse(eventID, model.AudioPulse{
		Source:           sourceID,
		SourceGeneration: event.SourceGeneration,
		Revision:         event.Revision,
		Index:            index,
		ObservedAt:       observedAt,
	}); err != nil {
		return model.LipSyncProfile{}, err
	}
	return coordinator.LipSyncStatus(eventID), nil
}

// LipSyncStatus 返回事件当前信号源代次与节目版本下的音画实测档案。
func (coordinator *Coordinator) LipSyncStatus(eventID model.EventID) model.LipSyncProfile {
	return coordinator.runtime.LipSyncProfile(eventID)
}

func (coordinator *Coordinator) requireEvent(eventID model.EventID) error {
	if _, ok := coordinator.runtime.Events.Get(eventID); !ok {
		return fmt.Errorf("event %s not found", eventID)
	}
	return nil
}

// resolveSource 返回事件当前正在播出/暂存的信号源；显式指定且有效时以指定值为准。
func (coordinator *Coordinator) resolveSource(eventID model.EventID, explicit model.SourceID) (model.SourceID, error) {
	if explicit.Valid() {
		return explicit, nil
	}
	if output, ok := coordinator.runtime.Playout.Output(eventID); ok {
		return output.Source, nil
	}
	if status, ok := coordinator.runtime.Sync.Status(eventID); ok && status.Source.Valid() {
		return status.Source, nil
	}
	return "", fmt.Errorf("event %s has no current signal source", eventID)
}

func (coordinator *Coordinator) ScheduleFailover(ctx context.Context, eventID model.EventID, delay time.Duration) (<-chan error, error) {
	event, ok := coordinator.runtime.Events.Get(eventID)
	if !ok {
		return nil, fmt.Errorf("event %s not found", eventID)
	}
	return coordinator.runtime.Backup.Schedule(ctx, event, "1080p50-backup", delay), nil
}

func (coordinator *Coordinator) CancelFailover(eventID model.EventID) error {
	if _, err := coordinator.runtime.EventFlow.CancelCommand(eventID); err != nil {
		return err
	}
	return coordinator.runtime.Backup.Cancel(eventID)
}
