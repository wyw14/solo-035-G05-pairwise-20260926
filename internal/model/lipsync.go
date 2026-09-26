package model

import "time"

// LipSyncStatus 描述一次音画同步实测是否足以支撑播出证明。
type LipSyncStatus string

const (
	// LipSyncPending 表示尚无足够的有效配对样本。
	LipSyncPending LipSyncStatus = "pending"
	// LipSyncAligned 表示有效样本充足且抖动落在容差区间内。
	LipSyncAligned LipSyncStatus = "aligned"
	// LipSyncJitter 表示样本充足但配对偏移抖动超出容差。
	LipSyncJitter LipSyncStatus = "jitter"
	// LipSyncStale 表示现存测量不属于当前信号源代次或节目版本。
	LipSyncStale LipSyncStatus = "stale"
)

// SyncMarker 是视频画面上同步标记被采集到的时刻，index 用于与同源的音频脉冲配对。
type SyncMarker struct {
	Source           SourceID   `json:"source"`
	SourceGeneration uint64     `json:"source_generation"`
	Revision         RevisionID `json:"revision"`
	Index            int64      `json:"index"`
	ObservedAt       time.Time  `json:"observed_at"`
}

// AudioPulse 是与视频标记对应的音频参考脉冲被采集到的时刻。
type AudioPulse struct {
	Source           SourceID   `json:"source"`
	SourceGeneration uint64     `json:"source_generation"`
	Revision         RevisionID `json:"revision"`
	Index            int64      `json:"index"`
	ObservedAt       time.Time  `json:"observed_at"`
}

// LipSyncSample 是一对成功配对的视频标记与音频脉冲，OffsetMillis 为音频相对画面的偏移。
type LipSyncSample struct {
	Index        int64   `json:"index"`
	OffsetMillis float64 `json:"offset_ms"`
}

// LipSyncProfile 是针对某个信号源代次与节目版本实测得到的音画偏移档案。
type LipSyncProfile struct {
	EventID          EventID         `json:"event_id"`
	Source           SourceID        `json:"source"`
	SourceGeneration uint64          `json:"source_generation"`
	Revision         RevisionID      `json:"revision"`
	Status           LipSyncStatus   `json:"status"`
	SampleCount      int             `json:"sample_count"`
	MarkerCount      int             `json:"marker_count"`
	PulseCount       int             `json:"pulse_count"`
	MissingPairs     int             `json:"missing_pairs"`
	OffsetMillis     float64         `json:"offset_ms"`
	MinOffsetMillis  float64         `json:"min_offset_ms"`
	MaxOffsetMillis  float64         `json:"max_offset_ms"`
	JitterMillis     float64         `json:"jitter_ms"`
	ToleranceMillis  float64         `json:"tolerance_ms"`
	Reason           string          `json:"reason"`
	FirstObservedAt  time.Time       `json:"first_observed_at"`
	LastObservedAt   time.Time       `json:"last_observed_at"`
	Samples          []LipSyncSample `json:"samples,omitempty"`
}

// Valid 表示档案对应当前代次与版本、样本充足且抖动可控，可作为播出证明。
func (profile LipSyncProfile) Valid() bool {
	return profile.Status == LipSyncAligned
}

// LipSyncProof 是写入播出日志的音画同步实测证明。
type LipSyncProof struct {
	EventID          EventID    `json:"event_id"`
	Source           SourceID   `json:"source"`
	SourceGeneration uint64     `json:"source_generation"`
	Revision         RevisionID `json:"revision"`
	Method           string     `json:"method"`
	OffsetMillis     float64    `json:"offset_ms"`
	MinOffsetMillis  float64    `json:"min_offset_ms"`
	MaxOffsetMillis  float64    `json:"max_offset_ms"`
	JitterMillis     float64    `json:"jitter_ms"`
	ToleranceMillis  float64    `json:"tolerance_ms"`
	SampleCount      int        `json:"sample_count"`
	MeasuredAt       time.Time  `json:"measured_at"`
}
