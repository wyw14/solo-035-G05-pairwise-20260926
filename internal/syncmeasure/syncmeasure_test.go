package syncmeasure

import (
	"fmt"
	"testing"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

func newTestCoordinator(t *testing.T, now time.Time) *Coordinator {
	t.Helper()
	archive := NewArchive()
	coordinator, err := NewCoordinator(
		archive,
		Tolerance{RequiredSamples: 3, JitterMillis: 40, MaxSampleAge: 10 * time.Second},
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	return coordinator
}

func measurementPair(source model.SourceID, generation uint64, revision model.RevisionID, marker string, videoAt time.Time, offset time.Duration) Measurement {
	return Measurement{
		Source:           source,
		SourceGeneration: generation,
		Revision:         revision,
		MarkerID:         marker,
		VideoAt:          videoAt,
		AudioAt:          videoAt.Add(offset),
		RecordedAt:       videoAt,
	}
}

func TestEstimateStableIntervalUsesMeasuredOffsets(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	coordinator := newTestCoordinator(t, now)
	eventID := model.NewEventID()
	source := model.NewSourceID()
	revision := model.NewRevisionID()

	base := now.Add(-time.Second)
	for i := 0; i < 3; i++ {
		videoAt := base.Add(time.Duration(i) * 100 * time.Millisecond)
		if _, err := coordinator.Record(eventID, measurementPair(source, 7, revision, fmt.Sprintf("m%d", i), videoAt, 12*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}

	profile := coordinator.Profile(eventID, source, revision, 7)
	current, ok := profile.Current()
	if !ok {
		t.Fatal("expected current group")
	}
	if !current.EligibleProof {
		t.Fatalf("stable current group must be eligible: %s", current.Estimate.Reason)
	}
	if current.Estimate.Status != StatusStable {
		t.Fatalf("status = %s", current.Estimate.Status)
	}
	if current.Estimate.OffsetMillis == nil || *current.Estimate.OffsetMillis != 12 {
		t.Fatalf("offset = %v, want 12ms", current.Estimate.OffsetMillis)
	}

	proof, _, err := coordinator.ProofForCurrent(eventID, source, revision, 7)
	if err != nil {
		t.Fatal(err)
	}
	if proof.Method != "measured-video-marker/audio-pulse" || proof.OffsetMillis != 12 || proof.SourceGeneration != 7 {
		t.Fatalf("proof = %+v", proof)
	}
	if proof.StableCount != 3 {
		t.Fatalf("stable count = %d", proof.StableCount)
	}
}

func TestEstimateJitterIsReported(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	coordinator := newTestCoordinator(t, now)
	eventID := model.NewEventID()
	source := model.NewSourceID()
	revision := model.NewRevisionID()

	offsets := []time.Duration{10 * time.Millisecond, 12 * time.Millisecond, 90 * time.Millisecond}
	base := now.Add(-time.Second)
	for i, offset := range offsets {
		videoAt := base.Add(time.Duration(i) * 100 * time.Millisecond)
		if _, err := coordinator.Record(eventID, measurementPair(source, 3, revision, fmt.Sprintf("m%d", i), videoAt, offset)); err != nil {
			t.Fatal(err)
		}
	}

	profile := coordinator.Profile(eventID, source, revision, 3)
	current, _ := profile.Current()
	if current.Estimate.Status != StatusJitter {
		t.Fatalf("status = %s, want jitter", current.Estimate.Status)
	}
	if current.EligibleProof {
		t.Fatal("jittering group must not be eligible proof")
	}
	if current.Estimate.OffsetMillis != nil {
		t.Fatal("jitter must not produce a fabricated offset")
	}
	if _, _, err := coordinator.ProofForCurrent(eventID, source, revision, 3); err == nil {
		t.Fatal("expected proof to be rejected for jitter")
	}
}

func TestMissingMeasurementsAreReported(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	coordinator := newTestCoordinator(t, now)
	eventID := model.NewEventID()
	source := model.NewSourceID()
	revision := model.NewRevisionID()

	profile := coordinator.Profile(eventID, source, revision, 5)
	current, ok := profile.Current()
	if !ok {
		t.Fatal("expected current group even without samples")
	}
	if current.Estimate.Status != StatusMissing || current.Estimate.SampleCount != 0 {
		t.Fatalf("estimate = %+v", current.Estimate)
	}
	if current.EligibleProof {
		t.Fatal("missing measurements cannot prove playout")
	}
}

func TestStaleMeasurementsDoNotProvePlayout(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	coordinator := newTestCoordinator(t, now)
	eventID := model.NewEventID()
	source := model.NewSourceID()
	revision := model.NewRevisionID()

	base := now.Add(-30 * time.Second)
	for i := 0; i < 3; i++ {
		videoAt := base.Add(time.Duration(i) * 100 * time.Millisecond)
		if _, err := coordinator.Record(eventID, measurementPair(source, 2, revision, fmt.Sprintf("m%d", i), videoAt, 8*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}
	profile := coordinator.Profile(eventID, source, revision, 2)
	current, _ := profile.Current()
	if current.Estimate.Status != StatusStale {
		t.Fatalf("status = %s, want stale", current.Estimate.Status)
	}
	if current.EligibleProof {
		t.Fatal("stale samples must not prove playout")
	}
}

func TestSupersededGenerationIsKeptButNotEligible(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	coordinator := newTestCoordinator(t, now)
	eventID := model.NewEventID()
	source := model.NewSourceID()
	revision := model.NewRevisionID()

	base := now.Add(-time.Second)
	for i := 0; i < 3; i++ {
		videoAt := base.Add(time.Duration(i) * 100 * time.Millisecond)
		if _, err := coordinator.Record(eventID, measurementPair(source, 4, revision, fmt.Sprintf("old%d", i), videoAt, 40*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}

	profile := coordinator.Profile(eventID, source, revision, 5)
	if len(profile.Groups) != 2 {
		t.Fatalf("groups = %d, want old generation retained plus empty current", len(profile.Groups))
	}
	for _, group := range profile.Groups {
		if group.Current {
			if group.EligibleProof || group.Estimate.Status != StatusMissing {
				t.Fatalf("current generation must be missing evidence: %+v", group)
			}
			continue
		}
		if !group.Superseded || group.EligibleProof {
			t.Fatalf("old generation must be shown but ineligible: %+v", group)
		}
	}

	if _, _, err := coordinator.ProofForCurrent(eventID, source, revision, 5); err == nil {
		t.Fatal("old generation measurements must not prove the current generation")
	}
}

func TestDuplicateMarkerRejected(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	coordinator := newTestCoordinator(t, now)
	eventID := model.NewEventID()
	source := model.NewSourceID()
	revision := model.NewRevisionID()
	videoAt := now.Add(-time.Second)
	measurement := measurementPair(source, 1, revision, "same-marker", videoAt, 5*time.Millisecond)
	if _, err := coordinator.Record(eventID, measurement); err != nil {
		t.Fatal(err)
	}
	if _, err := coordinator.Record(eventID, measurement); err == nil {
		t.Fatal("duplicate marker must be rejected")
	}
}

func TestInvalidMeasurementRejected(t *testing.T) {
	coordinator := newTestCoordinator(t, time.Unix(1000, 0).UTC())
	eventID := model.NewEventID()
	valid := Measurement{
		Source:           model.NewSourceID(),
		SourceGeneration: 1,
		Revision:         model.NewRevisionID(),
		MarkerID:         "m",
		VideoAt:          time.Unix(1, 0),
		AudioAt:          time.Unix(1, 0).Add(time.Millisecond),
		RecordedAt:       time.Unix(1, 0),
	}
	cases := map[string]func(*Measurement){
		"zero generation":  func(m *Measurement) { m.SourceGeneration = 0 },
		"empty marker":     func(m *Measurement) { m.MarkerID = "" },
		"zero video time":  func(m *Measurement) { m.VideoAt = time.Time{} },
		"zero audio time":  func(m *Measurement) { m.AudioAt = time.Time{} },
		"invalid source":   func(m *Measurement) { m.Source = model.SourceID("not-a-uuid") },
		"invalid revision": func(m *Measurement) { m.Revision = model.RevisionID("not-a-uuid") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			measurement := valid
			mutate(&measurement)
			if _, err := coordinator.Record(eventID, measurement); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestArchiveRecoveryRoundTrip(t *testing.T) {
	now := time.Unix(1000, 0).UTC()
	coordinator := newTestCoordinator(t, now)
	eventID := model.NewEventID()
	source := model.NewSourceID()
	revision := model.NewRevisionID()
	for i := 0; i < 3; i++ {
		videoAt := now.Add(-time.Second).Add(time.Duration(i) * 100 * time.Millisecond)
		if _, err := coordinator.Record(eventID, measurementPair(source, 1, revision, fmt.Sprintf("m%d", i), videoAt, 6*time.Millisecond)); err != nil {
			t.Fatal(err)
		}
	}

	exported := coordinator.archive.ExportAll()
	if len(exported) != 1 || len(exported[0].Measurements) != 3 {
		t.Fatalf("export = %+v", exported)
	}
	fresh := NewArchive()
	if err := fresh.Restore(exported); err != nil {
		t.Fatal(err)
	}
	items := fresh.List(eventID)
	if len(items) != 3 || items[0].OffsetMs != 6 {
		t.Fatalf("restored items = %+v", items)
	}
}
