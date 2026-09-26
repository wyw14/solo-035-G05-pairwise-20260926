package syncmeasure

import (
	"fmt"
	"sort"
	"time"

	"github.com/wyw14/solo-035/internal/model"
)

// GroupProfile is the measured profile for one source/revision/generation
// triple. Source and revision are the required grouping dimensions; the
// generation decides whether the group can still prove what is on air.
type GroupProfile struct {
	Group         Group    `json:"group"`
	Generation    uint64   `json:"generation"`
	Current       bool     `json:"current"`
	Superseded    bool     `json:"superseded"`
	EligibleProof bool     `json:"eligible_proof"`
	Estimate      Estimate `json:"estimate"`
}

// Profile reports every measured source/revision group. Only the group
// matching the source, revision and generation currently on air is eligible
// to back a playout proof.
type Profile struct {
	EventID           model.EventID    `json:"event_id"`
	CurrentSource     model.SourceID   `json:"current_source"`
	CurrentRevision   model.RevisionID `json:"current_revision"`
	CurrentGeneration uint64           `json:"current_generation"`
	Groups            []GroupProfile   `json:"groups"`
	GeneratedAt       time.Time        `json:"generated_at"`
}

// Current returns the profile of the current source/revision/generation.
func (profile Profile) Current() (GroupProfile, bool) {
	for _, group := range profile.Groups {
		if group.Current {
			return group, true
		}
	}
	return GroupProfile{}, false
}

// Proof is the measured evidence written into the playout journal. It is only
// produced when the current generation has a stable, fresh interval.
type Proof struct {
	Method           string           `json:"method"`
	Source           model.SourceID   `json:"source"`
	SourceGeneration uint64           `json:"source_generation"`
	Revision         model.RevisionID `json:"revision"`
	SampleCount      int              `json:"sample_count"`
	StableCount      int              `json:"stable_count"`
	OffsetMillis     float64          `json:"offset_ms"`
	LowerMillis      float64          `json:"lower_ms"`
	UpperMillis      float64          `json:"upper_ms"`
	JitterMillis     float64          `json:"jitter_ms"`
	FirstMeasuredAt  time.Time        `json:"first_measured_at"`
	LastMeasuredAt   time.Time        `json:"last_measured_at"`
}

type bucketKey struct {
	source     model.SourceID
	revision   model.RevisionID
	generation uint64
}

type Coordinator struct {
	archive   *Archive
	tolerance Tolerance
	clock     func() time.Time
}

func NewCoordinator(archive *Archive, tolerance Tolerance, clock func() time.Time) (*Coordinator, error) {
	if err := tolerance.validate(); err != nil {
		return nil, err
	}
	if archive == nil {
		return nil, fmt.Errorf("sync measurement archive is required")
	}
	if clock == nil {
		clock = time.Now
	}
	return &Coordinator{archive: archive, tolerance: tolerance, clock: clock}, nil
}

func (coordinator *Coordinator) Tolerance() Tolerance {
	return coordinator.tolerance
}

func (coordinator *Coordinator) Record(eventID model.EventID, measurement Measurement) (RecordedMeasurement, error) {
	if measurement.RecordedAt.IsZero() {
		measurement.RecordedAt = coordinator.clock()
	}
	return coordinator.archive.Add(eventID, measurement)
}

func (coordinator *Coordinator) Measurements(eventID model.EventID) []RecordedMeasurement {
	return coordinator.archive.List(eventID)
}

// Profile groups the archived measurements by signal source, program revision
// and source generation.
func (coordinator *Coordinator) Profile(eventID model.EventID, currentSource model.SourceID, currentRevision model.RevisionID, currentGeneration uint64) Profile {
	now := coordinator.clock().UTC()
	profile := Profile{
		EventID:           eventID,
		CurrentSource:     currentSource,
		CurrentRevision:   currentRevision,
		CurrentGeneration: currentGeneration,
		GeneratedAt:       now,
	}

	buckets := make(map[bucketKey][]RecordedMeasurement)
	order := make([]bucketKey, 0)
	for _, item := range coordinator.archive.List(eventID) {
		key := bucketKey{source: item.Source, revision: item.Revision, generation: item.SourceGeneration}
		if _, exists := buckets[key]; !exists {
			order = append(order, key)
		}
		buckets[key] = append(buckets[key], item)
	}
	currentKey := bucketKey{source: currentSource, revision: currentRevision, generation: currentGeneration}
	if _, exists := buckets[currentKey]; !exists {
		order = append(order, currentKey)
		buckets[currentKey] = nil
	}

	sort.Slice(order, func(i, j int) bool {
		if order[i].source != order[j].source {
			return order[i].source < order[j].source
		}
		if order[i].revision != order[j].revision {
			return order[i].revision < order[j].revision
		}
		return order[i].generation < order[j].generation
	})

	for _, key := range order {
		group := Group{EventID: eventID, Source: key.source, Revision: key.revision}
		groupEstimate := estimate(group, key.generation, buckets[key], coordinator.tolerance, now)
		groupProfile := GroupProfile{Group: group, Generation: key.generation, Estimate: groupEstimate}
		switch {
		case key == currentKey:
			groupProfile.Current = true
			groupProfile.EligibleProof = groupEstimate.Status == StatusStable
		case key.source == currentSource && key.revision == currentRevision:
			groupProfile.Superseded = true
			groupEstimate.Reason = fmt.Sprintf("measurements belong to superseded source generation %d; current generation is %d", key.generation, currentGeneration)
			groupProfile.Estimate = groupEstimate
		default:
			groupProfile.Superseded = true
			groupEstimate.Reason = fmt.Sprintf("measurements belong to source %s / revision %s, not the program on air", key.source, key.revision)
			groupProfile.Estimate = groupEstimate
		}
		profile.Groups = append(profile.Groups, groupProfile)
	}
	return profile
}

// ProofForCurrent returns measured evidence only when the current generation
// has a stable interval. Missing samples, stale samples and jitter are
// rejected with the estimate so callers can surface the exact failure.
func (coordinator *Coordinator) ProofForCurrent(eventID model.EventID, currentSource model.SourceID, currentRevision model.RevisionID, currentGeneration uint64) (Proof, Estimate, error) {
	profile := coordinator.Profile(eventID, currentSource, currentRevision, currentGeneration)
	current, ok := profile.Current()
	if !ok {
		return Proof{}, Estimate{}, fmt.Errorf("no sync measurement profile for the current generation")
	}
	groupEstimate := current.Estimate
	if groupEstimate.Status != StatusStable || groupEstimate.OffsetMillis == nil {
		return Proof{}, groupEstimate, fmt.Errorf("sync evidence is not provable for source generation %d: %s", currentGeneration, groupEstimate.Reason)
	}
	proof := Proof{
		Method:           "measured-video-marker/audio-pulse",
		Source:           currentSource,
		SourceGeneration: currentGeneration,
		Revision:         currentRevision,
		SampleCount:      groupEstimate.SampleCount,
		StableCount:      groupEstimate.StableCount,
		OffsetMillis:     *groupEstimate.OffsetMillis,
		LowerMillis:      *groupEstimate.LowerMillis,
		UpperMillis:      *groupEstimate.UpperMillis,
		JitterMillis:     *groupEstimate.JitterMillis,
		FirstMeasuredAt:  *groupEstimate.FirstMeasuredAt,
		LastMeasuredAt:   *groupEstimate.LastMeasuredAt,
	}
	return proof, groupEstimate, nil
}
