package service

import (
	"github.com/wyw14/solo-035/internal/syncmeasure"
)

// SyncProfiles returns measured sync profiles for every known event. Groups
// with missing samples or jitter are included so operators can see exactly
// why a generation cannot be proven.
func (runtime *Runtime) SyncProfiles() []syncmeasure.Profile {
	coordinator := NewCoordinator(runtime)
	events := runtime.Events.List()
	profiles := make([]syncmeasure.Profile, 0, len(events))
	for _, event := range events {
		profile, err := coordinator.SyncProfile(event.ID)
		if err != nil {
			continue
		}
		profiles = append(profiles, profile)
	}
	return profiles
}
