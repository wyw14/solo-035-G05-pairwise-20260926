package lipsync

import (
	"testing"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

func testIdentities() (model.EventID, model.SourceID, model.RevisionID) {
	return model.NewEventID(), model.NewSourceID(), model.NewRevisionID()
}

func feedPair(t *testing.T, archive *Archive, eventID model.EventID, source model.SourceID, generation uint64, revision model.RevisionID, base time.Time, index int64, offset time.Duration) {
	t.Helper()
	markerAt := base.Add(time.Duration(index) * 40 * time.Millisecond)
	if err := archive.RecordMarker(eventID, model.SyncMarker{Source: source, SourceGeneration: generation, Revision: revision, Index: index, ObservedAt: markerAt}); err != nil {
		t.Fatalf("record marker: %v", err)
	}
	if err := archive.RecordPulse(eventID, model.AudioPulse{Source: source, SourceGeneration: generation, Revision: revision, Index: index, ObservedAt: markerAt.Add(offset)}); err != nil {
		t.Fatalf("record pulse: %v", err)
	}
}

func TestArchiveEstimatesOffsetAndStableRange(t *testing.T) {
	archive, err := NewArchive(Config{RequiredSamples: 3, MaxJitterMillis: 40})
	if err != nil {
		t.Fatal(err)
	}
	eventID, source, revision := testIdentities()
	base := time.Unix(1000, 0).UTC()
	feedPair(t, archive, eventID, source, 7, revision, base, 1, 10*time.Millisecond)
	feedPair(t, archive, eventID, source, 7, revision, base, 2, 14*time.Millisecond)
	feedPair(t, archive, eventID, source, 7, revision, base, 3, 12*time.Millisecond)

	profile := archive.Evaluate(eventID, source, 7, revision)
	if profile.Status != model.LipSyncAligned {
		t.Fatalf("status = %s (%s)", profile.Status, profile.Reason)
	}
	if profile.SampleCount != 3 {
		t.Fatalf("sample count = %d", profile.SampleCount)
	}
	if profile.OffsetMillis != 12.0 {
		t.Fatalf("offset = %v, want 12", profile.OffsetMillis)
	}
	if profile.MinOffsetMillis != 10.0 || profile.MaxOffsetMillis != 14.0 {
		t.Fatalf("range = %v..%v, want 10..14", profile.MinOffsetMillis, profile.MaxOffsetMillis)
	}
	if profile.JitterMillis != 4.0 {
		t.Fatalf("jitter = %v, want 4", profile.JitterMillis)
	}
	if profile.MissingPairs != 0 {
		t.Fatalf("missing pairs = %d, want 0", profile.MissingPairs)
	}

	proof, _, err := archive.Proof(eventID, source, 7, revision, base)
	if err != nil {
		t.Fatalf("proof: %v", err)
	}
	if proof.OffsetMillis != 12.0 || proof.SampleCount != 3 || proof.SourceGeneration != 7 || proof.Revision != revision {
		t.Fatalf("proof does not carry measured data: %+v", proof)
	}
	if proof.Method == "" {
		t.Fatal("proof method is empty")
	}
}

func TestArchiveReportsMissingMeasurements(t *testing.T) {
	archive, _ := NewArchive(DefaultConfig())
	eventID, source, revision := testIdentities()
	base := time.Unix(1000, 0).UTC()

	// 完全缺测。
	if profile := archive.Evaluate(eventID, source, 1, revision); profile.Status != model.LipSyncPending {
		t.Fatalf("empty status = %s", profile.Status)
	}

	// 只有视频标记。
	if err := archive.RecordMarker(eventID, model.SyncMarker{Source: source, SourceGeneration: 1, Revision: revision, Index: 1, ObservedAt: base}); err != nil {
		t.Fatal(err)
	}
	if profile := archive.Evaluate(eventID, source, 1, revision); profile.Status != model.LipSyncPending || profile.MissingPairs != 1 {
		t.Fatalf("marker-only profile = %+v", profile)
	}

	// 只有音频脉冲、索引不配对。
	if err := archive.RecordPulse(eventID, model.AudioPulse{Source: source, SourceGeneration: 1, Revision: revision, Index: 99, ObservedAt: base.Add(time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	profile := archive.Evaluate(eventID, source, 1, revision)
	if profile.Status != model.LipSyncPending || profile.SampleCount != 0 || profile.MissingPairs != 2 {
		t.Fatalf("unpaired profile = %+v", profile)
	}
	if _, _, err := archive.Proof(eventID, source, 1, revision, base); err == nil {
		t.Fatal("proof must be rejected while measurements are missing")
	}
}

func TestArchiveRejectsInsufficientSamples(t *testing.T) {
	archive, _ := NewArchive(Config{RequiredSamples: 3, MaxJitterMillis: 40})
	eventID, source, revision := testIdentities()
	base := time.Unix(1000, 0).UTC()
	feedPair(t, archive, eventID, source, 1, revision, base, 1, 12*time.Millisecond)
	feedPair(t, archive, eventID, source, 1, revision, base, 2, 12*time.Millisecond)
	profile := archive.Evaluate(eventID, source, 1, revision)
	if profile.Status != model.LipSyncPending {
		t.Fatalf("status = %s, want pending", profile.Status)
	}
}

func TestArchiveFlagsExcessiveJitter(t *testing.T) {
	archive, _ := NewArchive(Config{RequiredSamples: 3, MaxJitterMillis: 40})
	eventID, source, revision := testIdentities()
	base := time.Unix(1000, 0).UTC()
	feedPair(t, archive, eventID, source, 1, revision, base, 1, 0*time.Millisecond)
	feedPair(t, archive, eventID, source, 1, revision, base, 2, 25*time.Millisecond)
	feedPair(t, archive, eventID, source, 1, revision, base, 3, 55*time.Millisecond)
	profile := archive.Evaluate(eventID, source, 1, revision)
	if profile.Status != model.LipSyncJitter {
		t.Fatalf("status = %s (%s), want jitter", profile.Status, profile.Reason)
	}
	if profile.JitterMillis != 55.0 {
		t.Fatalf("jitter = %v, want 55", profile.JitterMillis)
	}
	if _, _, err := archive.Proof(eventID, source, 1, revision, base); err == nil {
		t.Fatal("jittery measurement must not produce a proof")
	}
}

func TestArchiveRejectsStaleGenerationAndRevision(t *testing.T) {
	archive, _ := NewArchive(DefaultConfig())
	eventID, source, revision := testIdentities()
	base := time.Unix(1000, 0).UTC()
	for i := int64(1); i <= 3; i++ {
		feedPair(t, archive, eventID, source, 1, revision, base, i, 12*time.Millisecond)
	}

	// 信号源代次推进后，旧测量不再是当前证明。
	staleByGeneration := archive.Evaluate(eventID, source, 2, revision)
	if staleByGeneration.Status != model.LipSyncStale {
		t.Fatalf("status = %s, want stale", staleByGeneration.Status)
	}

	// 节目版本变化同样使旧测量失效。
	newRevision := model.NewRevisionID()
	staleByRevision := archive.Evaluate(eventID, source, 1, newRevision)
	if staleByRevision.Status != model.LipSyncStale {
		t.Fatalf("status = %s, want stale", staleByRevision.Status)
	}
	if _, _, err := archive.Proof(eventID, source, 2, revision, base); err == nil {
		t.Fatal("stale generation must not produce a proof")
	}
}

func TestArchiveDiscardsOldGenerationWhenNewMeasurementArrives(t *testing.T) {
	archive, _ := NewArchive(DefaultConfig())
	eventID, source, revision := testIdentities()
	base := time.Unix(1000, 0).UTC()
	feedPair(t, archive, eventID, source, 1, revision, base, 1, 12*time.Millisecond)
	feedPair(t, archive, eventID, source, 1, revision, base, 2, 12*time.Millisecond)

	// 新一代次的观测到达后，旧代次样本作废，从零累计。
	feedPair(t, archive, eventID, source, 2, revision, base, 1, 12*time.Millisecond)
	profile := archive.Evaluate(eventID, source, 2, revision)
	if profile.Status != model.LipSyncPending || profile.SampleCount != 1 {
		t.Fatalf("profile after generation change = %+v", profile)
	}
}

func TestArchiveRecoveryRoundTrip(t *testing.T) {
	archive, _ := NewArchive(Config{RequiredSamples: 3, MaxJitterMillis: 40})
	eventID, source, revision := testIdentities()
	base := time.Unix(1000, 0).UTC()
	for i := int64(1); i <= 3; i++ {
		feedPair(t, archive, eventID, source, 5, revision, base, i, time.Duration(10+i)*time.Millisecond)
	}
	exported := archive.Export()
	if len(exported) != 1 || exported[0].SourceGeneration != 5 {
		t.Fatalf("export = %+v", exported)
	}

	restored, _ := NewArchive(Config{RequiredSamples: 3, MaxJitterMillis: 40})
	if err := restored.Restore(exported); err != nil {
		t.Fatalf("restore: %v", err)
	}
	profile := restored.Evaluate(eventID, source, 5, revision)
	if profile.Status != model.LipSyncAligned || profile.SampleCount != 3 || profile.OffsetMillis != 12.0 {
		t.Fatalf("restored profile = %+v", profile)
	}
}
