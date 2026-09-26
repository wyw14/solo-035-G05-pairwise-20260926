package api

import "net/http"

func (server *Server) lipSync(writer http.ResponseWriter, request *http.Request) {
	profiles := server.runtime.LipSyncProfiles()
	writeJSON(writer, http.StatusOK, map[string]any{
		"captured_at": server.runtime.Clock().UTC(),
		"lip_sync":    profiles,
	})
}
