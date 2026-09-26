package service

import (
	"testing"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

// prepareOnAir 复现开播前的全部联锁准备；measured 决定是否注入音画实测样本。
func prepareOnAir(t *testing.T, runtime *Runtime, measured bool) (*Coordinator, model.EventID) {
	t.Helper()
	coordinator := NewCoordinator(runtime)
	event, err := coordinator.CreateEvent("Lip Sync Test")
	if err != nil {
		t.Fatal(err)
	}
	sourceID := model.NewSourceID()
	status, err := coordinator.PrepareSignal(event.ID, sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if err := coordinator.ConfirmSignal(sourceID, status.CurrentCommand); err != nil {
		t.Fatal(err)
	}
	base := runtime.Clock().UTC()
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
	if measured {
		for sequence := int64(1); sequence <= 3; sequence++ {
			markerAt := base.Add(time.Duration(sequence) * 40 * time.Millisecond)
			if _, err := coordinator.RecordSyncMarker(event.ID, sourceID, sequence, markerAt); err != nil {
				t.Fatal(err)
			}
			if _, err := coordinator.RecordAudioPulse(event.ID, sourceID, sequence, markerAt.Add(12*time.Millisecond)); err != nil {
				t.Fatal(err)
			}
		}
	}
	return coordinator, event.ID
}

func TestCommitOnAirRequiresMeasuredLipSync(t *testing.T) {
	runtime, err := NewRuntime(t.TempDir(), func() time.Time { return time.Unix(100, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	coordinator, eventID := prepareOnAir(t, runtime, false)

	output, ok := runtime.Playout.Output(eventID)
	if !ok {
		t.Fatal("output not staged")
	}
	if err := coordinator.CommitOnAir(eventID, output.Fence); err == nil {
		t.Fatal("CommitOnAir must be rejected without a current lip-sync measurement")
	}

	// 缺测不得留下任何 lip-sync-proof 记录。
	for _, record := range runtime.Journal.Records() {
		if record.Kind == "lip-sync-proof" {
			t.Fatalf("missing measurement must not be journaled as proof: %s", record.ID)
		}
	}

	profile := coordinator.LipSyncStatus(eventID)
	if profile.Status != model.LipSyncPending {
		t.Fatalf("profile status = %s, want pending", profile.Status)
	}
}

func TestCommitOnAirPersistsMeasuredLipSyncProof(t *testing.T) {
	runtime, err := NewRuntime(t.TempDir(), func() time.Time { return time.Unix(100, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	coordinator, eventID := prepareOnAir(t, runtime, true)
	output, _ := runtime.Playout.Output(eventID)
	if err := coordinator.CommitOnAir(eventID, output.Fence); err != nil {
		t.Fatalf("CommitOnAir: %v", err)
	}

	var proof map[string]any
	for _, record := range runtime.Journal.Records() {
		if record.Kind != "lip-sync-proof" {
			continue
		}
		if err := record.Decode(&proof); err != nil {
			t.Fatal(err)
		}
	}
	if proof == nil {
		t.Fatal("no lip-sync-proof record was persisted")
	}
	if proof["offset_ms"] != 12.0 {
		t.Fatalf("offset_ms = %v, want measured 12", proof["offset_ms"])
	}
	if proof["sample_count"] != float64(3) {
		t.Fatalf("sample_count = %v, want 3", proof["sample_count"])
	}
	if proof["jitter_ms"] != float64(0) {
		t.Fatalf("jitter_ms = %v, want 0", proof["jitter_ms"])
	}
	if proof["method"] == "" || proof["method"] == nil {
		t.Fatal("proof must record its measurement method")
	}
	if proof["source_generation"] == nil {
		t.Fatal("proof must carry the source generation")
	}
}

func TestCommitOnAirRejectsStaleProgramRevisionMeasurement(t *testing.T) {
	runtime, err := NewRuntime(t.TempDir(), func() time.Time { return time.Unix(100, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	coordinator, eventID := prepareOnAir(t, runtime, false)
	source, _ := runtime.currentLipSyncSource(eventID)
	event, _ := runtime.Events.Get(eventID)

	// 直接注入一组对齐但属于"上一节目版本"的测量（代次相同、版本不同）。
	staleRevision := model.NewRevisionID()
	base := runtime.Clock().UTC()
	for sequence := int64(1); sequence <= 3; sequence++ {
		markerAt := base.Add(time.Duration(sequence) * 40 * time.Millisecond)
		if err := runtime.LipSync.RecordMarker(eventID, model.SyncMarker{
			Source: source, SourceGeneration: event.SourceGeneration, Revision: staleRevision,
			Index: sequence, ObservedAt: markerAt,
		}); err != nil {
			t.Fatal(err)
		}
		if err := runtime.LipSync.RecordPulse(eventID, model.AudioPulse{
			Source: source, SourceGeneration: event.SourceGeneration, Revision: staleRevision,
			Index: sequence, ObservedAt: markerAt.Add(12 * time.Millisecond),
		}); err != nil {
			t.Fatal(err)
		}
	}

	profile := coordinator.LipSyncStatus(eventID)
	if profile.Status != model.LipSyncStale {
		t.Fatalf("profile status = %s (%s), want stale", profile.Status, profile.Reason)
	}
	output, _ := runtime.Playout.Output(eventID)
	if err := coordinator.CommitOnAir(eventID, output.Fence); err == nil {
		t.Fatal("a stable measurement from a previous program revision must not certify on-air")
	}
}

func TestBootstrapStillReachesOnAirWithMeasuredProof(t *testing.T) {
	runtime, err := NewRuntime(t.TempDir(), func() time.Time { return time.Unix(100, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	if err := Bootstrap(runtime); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	profiles := runtime.LipSyncProfiles()
	if len(profiles) != 1 || profiles[0].Status != model.LipSyncAligned {
		t.Fatalf("profiles = %+v", profiles)
	}
}
