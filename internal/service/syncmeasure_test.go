package service

import (
	"strings"
	"testing"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

func prepareEvent(t *testing.T, coordinator *Coordinator) (model.LiveEvent, model.SourceID) {
	t.Helper()
	created, err := coordinator.CreateEvent("Sync Proof Event")
	if err != nil {
		t.Fatal(err)
	}
	sourceID := model.NewSourceID()
	status, err := coordinator.PrepareSignal(created.ID, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.ConfirmSignal(sourceID, status.CurrentCommand); err != nil {
		t.Fatal(err)
	}
	event, ok := coordinator.runtime.Events.Get(created.ID)
	if !ok {
		t.Fatal("event missing")
	}
	base := coordinator.runtime.Clock().UTC()
	for sequence := int64(1); sequence <= 3; sequence++ {
		frame := model.Frame{
			Source:           sourceID,
			SourceGeneration: status.Generation,
			Sequence:         sequence,
			Timecode:         1000 + sequence,
			CapturedAt:       base.Add(time.Duration(sequence) * 20 * time.Millisecond),
			VideoLocked:      true,
			AudioLocked:      true,
		}
		if err := coordinator.PushFrame(event.ID, frame); err != nil {
			t.Fatal(err)
		}
	}
	if err := coordinator.ConfigureProgram(event.ID, sourceID); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.EstablishInterlocks(event.ID); err != nil {
		t.Fatal(err)
	}
	return event, sourceID
}

func recordOffsets(t *testing.T, coordinator *Coordinator, eventID model.EventID, sourceID model.SourceID, offsets ...time.Duration) {
	t.Helper()
	base := coordinator.runtime.Clock().UTC().Add(-time.Second)
	for i, offset := range offsets {
		videoAt := base.Add(time.Duration(i) * 100 * time.Millisecond)
		if _, err := coordinator.RecordSyncMeasurement(eventID, sourceID, markerID(i), videoAt, videoAt.Add(offset)); err != nil {
			t.Fatal(err)
		}
	}
}

func markerID(i int) string {
	return "marker-" + string(rune('a'+i))
}

func TestCommitOnAirPersistsMeasuredProof(t *testing.T) {
	runtime, err := NewRuntime(t.TempDir(), func() time.Time { return time.Unix(100, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewCoordinator(runtime)
	event, sourceID := prepareEvent(t, coordinator)
	recordOffsets(t, coordinator, event.ID, sourceID, 12*time.Millisecond, 13*time.Millisecond, 11*time.Millisecond)

	output, ok := runtime.Playout.Output(event.ID)
	if !ok {
		t.Fatal("output not staged")
	}
	if err := coordinator.CommitOnAir(event.ID, output.Fence); err != nil {
		t.Fatalf("commit with measured proof: %v", err)
	}

	var proofRecord *struct {
		Method           string  `json:"method"`
		SourceGeneration uint64  `json:"source_generation"`
		OffsetMillis     float64 `json:"offset_ms"`
		LowerMillis      float64 `json:"lower_ms"`
		UpperMillis      float64 `json:"upper_ms"`
		StableCount      int     `json:"stable_count"`
	}
	for _, record := range runtime.Journal.Records() {
		if record.Kind != "lip-sync-proof" {
			continue
		}
		proof := &struct {
			Method           string  `json:"method"`
			SourceGeneration uint64  `json:"source_generation"`
			OffsetMillis     float64 `json:"offset_ms"`
			LowerMillis      float64 `json:"lower_ms"`
			UpperMillis      float64 `json:"upper_ms"`
			StableCount      int     `json:"stable_count"`
		}{}
		if err := record.Decode(proof); err != nil {
			t.Fatal(err)
		}
		proofRecord = proof
	}
	if proofRecord == nil {
		t.Fatal("no lip-sync proof journaled")
	}
	if proofRecord.Method != "measured-video-marker/audio-pulse" {
		t.Fatalf("method = %q", proofRecord.Method)
	}
	if proofRecord.SourceGeneration != event.SourceGeneration {
		t.Fatalf("proof generation = %d, event generation = %d", proofRecord.SourceGeneration, event.SourceGeneration)
	}
	if proofRecord.OffsetMillis < 11 || proofRecord.OffsetMillis > 13 {
		t.Fatalf("measured offset = %v", proofRecord.OffsetMillis)
	}
	if proofRecord.StableCount != 3 {
		t.Fatalf("stable count = %d", proofRecord.StableCount)
	}
	if !runtime.Diagnostics().Healthy {
		t.Fatal("diagnostics must pass with a measured proof")
	}
}

func TestCommitOnAirRejectsMissingMeasurements(t *testing.T) {
	runtime, err := NewRuntime(t.TempDir(), func() time.Time { return time.Unix(100, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewCoordinator(runtime)
	event, _ := prepareEvent(t, coordinator)
	output, _ := runtime.Playout.Output(event.ID)

	err = coordinator.CommitOnAir(event.ID, output.Fence)
	if err == nil || !strings.Contains(err.Error(), "no video-marker/audio-pulse measurement") {
		t.Fatalf("err = %v, want missing measurement rejection", err)
	}
}

func TestCommitOnAirRejectsJitter(t *testing.T) {
	runtime, err := NewRuntime(t.TempDir(), func() time.Time { return time.Unix(100, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewCoordinator(runtime)
	event, sourceID := prepareEvent(t, coordinator)
	recordOffsets(t, coordinator, event.ID, sourceID, 10*time.Millisecond, 12*time.Millisecond, 90*time.Millisecond)
	output, _ := runtime.Playout.Output(event.ID)

	err = coordinator.CommitOnAir(event.ID, output.Fence)
	if err == nil || !strings.Contains(err.Error(), "not provable") {
		t.Fatalf("err = %v, want jitter rejection", err)
	}
}

func TestOldGenerationMeasurementsDoNotProveNewGeneration(t *testing.T) {
	runtime, err := NewRuntime(t.TempDir(), func() time.Time { return time.Unix(100, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewCoordinator(runtime)
	event, sourceID := prepareEvent(t, coordinator)
	recordOffsets(t, coordinator, event.ID, sourceID, 12*time.Millisecond, 12*time.Millisecond, 12*time.Millisecond)

	advanced := event.NewSource(runtime.Clock())
	if err := runtime.Events.Update(advanced); err != nil {
		t.Fatal(err)
	}
	output, _ := runtime.Playout.Output(event.ID)
	err = coordinator.CommitOnAir(event.ID, output.Fence)
	if err == nil || !strings.Contains(err.Error(), "source generation") {
		t.Fatalf("err = %v, want current-generation rejection", err)
	}

	profile, err := coordinator.SyncProfile(event.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Groups) != 2 {
		t.Fatalf("groups = %d, old generation must remain visible", len(profile.Groups))
	}
	current, ok := profile.Current()
	if !ok || current.EligibleProof || current.Estimate.Status != "missing" {
		t.Fatalf("current profile = %+v", current)
	}
}

func TestSyncProfilesExposedFromRuntime(t *testing.T) {
	runtime, err := NewRuntime(t.TempDir(), func() time.Time { return time.Unix(100, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(runtime); err != nil {
		t.Fatal(err)
	}
	profiles := runtime.SyncProfiles()
	if len(profiles) != 1 {
		t.Fatalf("profiles = %d", len(profiles))
	}
	current, ok := profiles[0].Current()
	if !ok || !current.EligibleProof {
		t.Fatalf("bootstrap profile must be provable: %+v", profiles[0])
	}
}

func TestCheckpointCarriesSyncMeasurements(t *testing.T) {
	runtime, err := NewRuntime(t.TempDir(), func() time.Time { return time.Unix(100, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	coordinator := NewCoordinator(runtime)
	event, sourceID := prepareEvent(t, coordinator)
	recordOffsets(t, coordinator, event.ID, sourceID, 12*time.Millisecond, 12*time.Millisecond, 12*time.Millisecond)
	output, _ := runtime.Playout.Output(event.ID)
	if err := coordinator.CommitOnAir(event.ID, output.Fence); err != nil {
		t.Fatal(err)
	}

	checkpoint := runtime.ExportCheckpoint()
	if len(checkpoint.SyncMeasurement) != 1 || len(checkpoint.SyncMeasurement[0].Measurements) != 3 {
		t.Fatalf("checkpoint measurements = %+v", checkpoint.SyncMeasurement)
	}

	fresh, err := NewRuntime(t.TempDir(), func() time.Time { return time.Unix(100, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.RestoreCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	profile, err := NewCoordinator(fresh).SyncProfile(event.ID)
	if err != nil {
		t.Fatal(err)
	}
	current, ok := profile.Current()
	if !ok || !current.EligibleProof {
		t.Fatalf("restored profile must retain provable measurements: %+v", profile)
	}
}
