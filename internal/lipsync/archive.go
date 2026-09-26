package lipsync

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

// Config 决定实测档案何时可以形成有效证明。
type Config struct {
	// RequiredSamples 是形成证明所需的最少视频/音频配对数。
	RequiredSamples int
	// MaxJitterMillis 是配对偏移之间允许的最大极差（抖动）。
	MaxJitterMillis float64
}

// Archive 保存各节目的音画同步实测档案，按信号源代次与节目版本归集。
type Archive struct {
	mu           sync.RWMutex
	config       Config
	accumulators map[model.EventID]*accumulator
}

func NewArchive(config Config) (*Archive, error) {
	if config.RequiredSamples < 2 {
		return nil, fmt.Errorf("lip-sync archive requires at least two paired samples")
	}
	if config.MaxJitterMillis <= 0 {
		return nil, fmt.Errorf("lip-sync jitter tolerance must be positive")
	}
	return &Archive{config: config, accumulators: make(map[model.EventID]*accumulator)}, nil
}

// DefaultConfig 给出与播出联锁相匹配的实测容差。
func DefaultConfig() Config {
	return Config{RequiredSamples: 3, MaxJitterMillis: 40}
}

// RecordMarker 接收一个视频同步标记的实测时间戳。
func (archive *Archive) RecordMarker(eventID model.EventID, marker model.SyncMarker) error {
	if err := validateIdentity(eventID, marker.Source, marker.SourceGeneration, marker.Revision); err != nil {
		return err
	}
	if marker.ObservedAt.IsZero() {
		return fmt.Errorf("video sync marker for event %s has no timestamp", eventID)
	}
	archive.mu.Lock()
	defer archive.mu.Unlock()
	archive.accumulatorFor(eventID, marker.Source, marker.SourceGeneration, marker.Revision).
		addMarker(marker.Index, marker.ObservedAt.UTC())
	return nil
}

// RecordPulse 接收一个音频参考脉冲的实测时间戳。
func (archive *Archive) RecordPulse(eventID model.EventID, pulse model.AudioPulse) error {
	if err := validateIdentity(eventID, pulse.Source, pulse.SourceGeneration, pulse.Revision); err != nil {
		return err
	}
	if pulse.ObservedAt.IsZero() {
		return fmt.Errorf("audio pulse for event %s has no timestamp", eventID)
	}
	archive.mu.Lock()
	defer archive.mu.Unlock()
	archive.accumulatorFor(eventID, pulse.Source, pulse.SourceGeneration, pulse.Revision).
		addPulse(pulse.Index, pulse.ObservedAt.UTC())
	return nil
}

// Evaluate 按"当前"信号源代次与节目版本评估档案：代次/版本不符的现存测量标记为 stale。
func (archive *Archive) Evaluate(eventID model.EventID, source model.SourceID, generation uint64, revision model.RevisionID) model.LipSyncProfile {
	archive.mu.RLock()
	defer archive.mu.RUnlock()
	acc := archive.accumulators[eventID]
	if acc == nil {
		return model.LipSyncProfile{
			EventID:          eventID,
			Source:           source,
			SourceGeneration: generation,
			Revision:         revision,
			Status:           model.LipSyncPending,
			ToleranceMillis:  archive.config.MaxJitterMillis,
			Reason:           "no lip-sync measurement recorded",
		}
	}
	profile := acc.profile(eventID, archive.config)
	if acc.source != source || acc.generation != generation || acc.revision != revision {
		profile.Status = model.LipSyncStale
		profile.Reason = fmt.Sprintf(
			"measurement is for source %s generation %d revision %s, not the current generation %d revision %s",
			acc.source, acc.generation, acc.revision, generation, revision)
	}
	return profile
}

// Proof 仅在档案对应当前代次/版本且实测稳定时返回播出证明。
func (archive *Archive) Proof(eventID model.EventID, source model.SourceID, generation uint64, revision model.RevisionID, now time.Time) (model.LipSyncProof, model.LipSyncProfile, error) {
	profile := archive.Evaluate(eventID, source, generation, revision)
	if profile.Status != model.LipSyncAligned {
		return model.LipSyncProof{}, profile, fmt.Errorf("lip-sync proof unavailable: %s", profile.Reason)
	}
	return model.LipSyncProof{
		EventID:          eventID,
		Source:           profile.Source,
		SourceGeneration: profile.SourceGeneration,
		Revision:         profile.Revision,
		Method:           "paired video marker / audio pulse measurement",
		OffsetMillis:     profile.OffsetMillis,
		MinOffsetMillis:  profile.MinOffsetMillis,
		MaxOffsetMillis:  profile.MaxOffsetMillis,
		JitterMillis:     profile.JitterMillis,
		ToleranceMillis:  profile.ToleranceMillis,
		SampleCount:      profile.SampleCount,
		MeasuredAt:       now.UTC(),
	}, profile, nil
}

// Profiles 返回档案中所有节目的当前实测档案（按事件标识排序）。
func (archive *Archive) Profiles() []model.LipSyncProfile {
	archive.mu.RLock()
	defer archive.mu.RUnlock()
	items := make([]model.LipSyncProfile, 0, len(archive.accumulators))
	for eventID, acc := range archive.accumulators {
		items = append(items, acc.profile(eventID, archive.config))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].EventID < items[j].EventID })
	return items
}

func (archive *Archive) Reset(eventID model.EventID) {
	archive.mu.Lock()
	defer archive.mu.Unlock()
	delete(archive.accumulators, eventID)
}

// accumulatorFor 返回对应代次/版本的累加器；来源、代次或版本变化时旧测量作废。
func (archive *Archive) accumulatorFor(eventID model.EventID, source model.SourceID, generation uint64, revision model.RevisionID) *accumulator {
	acc := archive.accumulators[eventID]
	if acc != nil && (acc.source != source || acc.generation != generation || acc.revision != revision) {
		acc = nil
		delete(archive.accumulators, eventID)
	}
	if acc == nil {
		acc = &accumulator{
			source:     source,
			generation: generation,
			revision:   revision,
			markers:    make(map[int64]time.Time),
			pulses:     make(map[int64]time.Time),
			paired:     make(map[int64]bool),
		}
		archive.accumulators[eventID] = acc
	}
	return acc
}

func validateIdentity(eventID model.EventID, source model.SourceID, generation uint64, revision model.RevisionID) error {
	if !eventID.Valid() {
		return fmt.Errorf("lip-sync measurement has an invalid event")
	}
	if !source.Valid() {
		return fmt.Errorf("lip-sync measurement for event %s has an invalid source", eventID)
	}
	if generation == 0 {
		return fmt.Errorf("lip-sync measurement for event %s has no source generation", eventID)
	}
	if !revision.Valid() {
		return fmt.Errorf("lip-sync measurement for event %s has an invalid program revision", eventID)
	}
	return nil
}

type accumulator struct {
	source      model.SourceID
	generation  uint64
	revision    model.RevisionID
	markers     map[int64]time.Time
	pulses      map[int64]time.Time
	paired      map[int64]bool
	samples     []model.LipSyncSample
	markerTotal int
	pulseTotal  int
	first       time.Time
	last        time.Time
}

func (acc *accumulator) addMarker(index int64, at time.Time) {
	acc.markerTotal++
	acc.touch(at)
	acc.markers[index] = at
	acc.tryPair(index)
}

func (acc *accumulator) addPulse(index int64, at time.Time) {
	acc.pulseTotal++
	acc.touch(at)
	acc.pulses[index] = at
	acc.tryPair(index)
}

// tryPair 在标记与脉冲都到齐时记录偏移；观测在 markers/pulses 中完整保留以便检查点回放。
func (acc *accumulator) tryPair(index int64) {
	if acc.paired[index] {
		return
	}
	markerAt, markerOK := acc.markers[index]
	pulseAt, pulseOK := acc.pulses[index]
	if !markerOK || !pulseOK {
		return
	}
	offset := float64(pulseAt.Sub(markerAt)) / float64(time.Millisecond)
	acc.samples = append(acc.samples, model.LipSyncSample{Index: index, OffsetMillis: offset})
	acc.paired[index] = true
}

func (acc *accumulator) touch(at time.Time) {
	if acc.first.IsZero() || at.Before(acc.first) {
		acc.first = at
	}
	if at.After(acc.last) {
		acc.last = at
	}
}

// unpairedCount 统计等待对侧信号的孤立标记与脉冲数量。
func (acc *accumulator) unpairedCount() int {
	count := 0
	for index := range acc.markers {
		if !acc.paired[index] {
			count++
		}
	}
	for index := range acc.pulses {
		if !acc.paired[index] {
			count++
		}
	}
	return count
}

func (acc *accumulator) profile(eventID model.EventID, config Config) model.LipSyncProfile {
	profile := model.LipSyncProfile{
		EventID:          eventID,
		Source:           acc.source,
		SourceGeneration: acc.generation,
		Revision:         acc.revision,
		SampleCount:      len(acc.samples),
		MarkerCount:      acc.markerTotal,
		PulseCount:       acc.pulseTotal,
		MissingPairs:     acc.unpairedCount(),
		ToleranceMillis:  config.MaxJitterMillis,
		FirstObservedAt:  acc.first,
		LastObservedAt:   acc.last,
	}
	if len(acc.samples) > 0 {
		sum := 0.0
		min := acc.samples[0].OffsetMillis
		max := min
		for _, sample := range acc.samples {
			sum += sample.OffsetMillis
			if sample.OffsetMillis < min {
				min = sample.OffsetMillis
			}
			if sample.OffsetMillis > max {
				max = sample.OffsetMillis
			}
		}
		profile.OffsetMillis = sum / float64(len(acc.samples))
		profile.MinOffsetMillis = min
		profile.MaxOffsetMillis = max
		profile.JitterMillis = max - min
	}
	switch {
	case len(acc.samples) < config.RequiredSamples:
		profile.Status = model.LipSyncPending
		profile.Reason = acc.missingReason(config.RequiredSamples)
	case profile.JitterMillis > config.MaxJitterMillis:
		profile.Status = model.LipSyncJitter
		profile.Reason = fmt.Sprintf(
			"offset jitter %.1fms exceeds tolerance %.1fms over %d paired samples",
			profile.JitterMillis, config.MaxJitterMillis, len(acc.samples))
	default:
		profile.Status = model.LipSyncAligned
		profile.Reason = fmt.Sprintf(
			"offset %.1fms with %.1fms spread over %d paired samples, within %.1fms tolerance",
			profile.OffsetMillis, profile.JitterMillis, len(acc.samples), config.MaxJitterMillis)
	}
	if len(acc.samples) > 0 {
		profile.Samples = append([]model.LipSyncSample(nil), acc.samples...)
		sort.Slice(profile.Samples, func(i, j int) bool { return profile.Samples[i].Index < profile.Samples[j].Index })
	}
	return profile
}

func (acc *accumulator) missingReason(required int) string {
	switch {
	case acc.markerTotal == 0 && acc.pulseTotal == 0:
		return "no lip-sync measurement recorded"
	case acc.markerTotal == 0:
		return "video sync markers are missing"
	case acc.pulseTotal == 0:
		return "audio reference pulses are missing"
	case len(acc.samples) == 0:
		return fmt.Sprintf("markers and pulses do not pair: %d signal(s) waiting for a counterpart", acc.unpairedCount())
	default:
		return fmt.Sprintf("only %d of %d required paired samples collected", len(acc.samples), required)
	}
}
