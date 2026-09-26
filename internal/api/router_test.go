package api

import (
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

func TestRouterServesLipSyncMeasurements(t *testing.T) {
	runtime, err := service.NewRuntime(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Bootstrap(runtime); err != nil {
		t.Fatal(err)
	}
	var web fs.FS = fstest.MapFS{"lipsync.html": &fstest.MapFile{Data: []byte("ok")}}
	request := httptest.NewRequest(http.MethodGet, "/api/lip-sync", nil)
	recorder := httptest.NewRecorder()
	NewServer(runtime, web).Router().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"status":"aligned"`) {
		t.Fatalf("lip-sync response lacks an aligned current measurement: %s", body)
	}
	if !strings.Contains(body, `"offset_ms":12`) {
		t.Fatalf("lip-sync response lacks the measured offset: %s", body)
	}
}
