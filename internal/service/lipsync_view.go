package service

import (
	"sort"

	"github.com/wyw14/solo-035/internal/model"
)

// currentLipSyncSource 解析事件当前的信号源：优先已暂存的播出输出，回退到同步窗口最近观测。
func (runtime *Runtime) currentLipSyncSource(eventID model.EventID) (model.SourceID, bool) {
	if output, ok := runtime.Playout.Output(eventID); ok && output.Source.Valid() {
		return output.Source, true
	}
	if status, ok := runtime.Sync.Status(eventID); ok && status.Source.Valid() {
		return status.Source, true
	}
	return "", false
}

// LipSyncProfile 返回事件当前信号源代次与节目版本下的音画实测档案。
func (runtime *Runtime) LipSyncProfile(eventID model.EventID) model.LipSyncProfile {
	event, ok := runtime.Events.Get(eventID)
	if !ok {
		return model.LipSyncProfile{EventID: eventID, Status: model.LipSyncPending, Reason: "event is not known"}
	}
	source, _ := runtime.currentLipSyncSource(eventID)
	return runtime.LipSync.Evaluate(eventID, source, event.SourceGeneration, event.Revision)
}

// LipSyncProfiles 返回所有事件的当前音画实测档案，缺测与抖动状态原样保留。
func (runtime *Runtime) LipSyncProfiles() []model.LipSyncProfile {
	events := runtime.Events.List()
	profiles := make([]model.LipSyncProfile, 0, len(events))
	for _, event := range events {
		profiles = append(profiles, runtime.LipSyncProfile(event.ID))
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].EventID < profiles[j].EventID })
	return profiles
}
