package api

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/wyw14/solo-035/internal/service"
)

func TestRouterServesOperationsAPI(t *testing.T) {
	runtime, err := service.NewRuntime(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Bootstrap(runtime); err != nil {
		t.Fatal(err)
	}
	var web fs.FS = fstest.MapFS{"operations.html": &fstest.MapFile{Data: []byte("ok")}}
	request := httptest.NewRequest(http.MethodGet, "/api/operations", nil)
	recorder := httptest.NewRecorder()
	NewServer(runtime, web).Router().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestRouterServesMeasuredSyncProfiles(t *testing.T) {
	runtime, err := service.NewRuntime(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Bootstrap(runtime); err != nil {
		t.Fatal(err)
	}
	var web fs.FS = fstest.MapFS{"operations.html": &fstest.MapFile{Data: []byte("ok")}}
	request := httptest.NewRequest(http.MethodGet, "/api/sync-profiles", nil)
	recorder := httptest.NewRecorder()
	NewServer(runtime, web).Router().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var payload struct {
		Profiles []struct {
			CurrentGeneration uint64 `json:"current_generation"`
			Groups            []struct {
				Current       bool `json:"current"`
				EligibleProof bool `json:"eligible_proof"`
				Estimate      struct {
					Status       string   `json:"status"`
					OffsetMillis *float64 `json:"offset_ms"`
				} `json:"estimate"`
			} `json:"groups"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Profiles) != 1 {
		t.Fatalf("profiles = %d", len(payload.Profiles))
	}
	found := false
	for _, group := range payload.Profiles[0].Groups {
		if group.Current {
			found = true
			if !group.EligibleProof || group.Estimate.Status != "stable" || group.Estimate.OffsetMillis == nil {
				t.Fatalf("current group = %+v", group)
			}
		}
	}
	if !found {
		t.Fatal("current generation group missing from profile")
	}
}

func TestRouterRejectsControlWithoutSyncedMeasurement(t *testing.T) {
	runtime, err := service.NewRuntime(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var web fs.FS = fstest.MapFS{"operations.html": &fstest.MapFile{Data: []byte("ok")}}
	body := strings.NewReader(`{"action":"record-sync-measurement"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/control", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	NewServer(runtime, web).Router().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want conflict for incomplete measurement", recorder.Code)
	}
}
