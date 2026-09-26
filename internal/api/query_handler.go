package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wyw14/solo-035/internal/model"
	"github.com/wyw14/solo-035/internal/service"
)

func (server *Server) health(writer http.ResponseWriter, request *http.Request) {
	report := server.runtime.Diagnostics()
	snapshot := server.runtime.Snapshot()
	status := http.StatusOK
	if !report.Healthy || !snapshot.Healthy() {
		status = http.StatusServiceUnavailable
	}
	writeJSON(writer, status, map[string]any{
		"status":     "available",
		"healthy":    report.Healthy,
		"checked_at": report.CheckedAt,
		"checks":     report.Checks,
		"failures":   report.Failures(),
	})
}

func (server *Server) operations(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"operations": server.runtime.Operations()})
}

func (server *Server) equipment(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"equipment": server.runtime.Equipment()})
}

func (server *Server) interlocks(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"interlocks": server.runtime.Interlocks()})
}

func (server *Server) incidents(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"incidents": server.runtime.Incidents()})
}

func (server *Server) snapshot(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, server.runtime.Snapshot())
}

func (server *Server) syncProfiles(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{"profiles": server.runtime.SyncProfiles()})
}

func (server *Server) syncProfile(writer http.ResponseWriter, request *http.Request) {
	eventID := model.EventID(chi.URLParam(request, "eventID"))
	coordinator := service.NewCoordinator(server.runtime)
	profile, err := coordinator.SyncProfile(eventID)
	if err != nil {
		writeError(writer, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, profile)
}
