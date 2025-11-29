package handlers

import (
	"net/http"
	"strings"
)

// SetupRoutes configures all HTTP routes for the application
func SetupRoutes(
	mux *http.ServeMux,
	machinesHandler *MachinesHandler,
	machineActionsHandler *MachineActionsHandler,
	usersHandler *UsersHandler,
	sseHandler *SSEHandler,
) {
	mux.HandleFunc("/", machinesHandler.List)
	mux.HandleFunc("/machines", machinesHandler.List)
	mux.HandleFunc("/machines/", func(w http.ResponseWriter, r *http.Request) {
		// Handle different machine actions based on URL path
		path := r.URL.Path
		if strings.HasSuffix(path, "/rename") {
			machinesHandler.Rename(w, r)
		} else if strings.HasSuffix(path, "/move") {
			machineActionsHandler.MoveNode(w, r)
		} else if strings.HasSuffix(path, "/tags") {
			machineActionsHandler.SetTags(w, r)
		} else if strings.HasSuffix(path, "/delete") {
			machineActionsHandler.DeleteNode(w, r)
		} else if strings.HasSuffix(path, "/expire") {
			machineActionsHandler.ExpireNode(w, r)
		} else if strings.HasSuffix(path, "/routes/exit-node/approve") {
			machineActionsHandler.ApproveExitNode(w, r)
		} else if strings.HasSuffix(path, "/routes/exit-node/reject") {
			machineActionsHandler.RejectExitNode(w, r)
		} else if strings.HasSuffix(path, "/routes/subnets/approve") {
			machineActionsHandler.ApproveSubnetRoute(w, r)
		} else if strings.HasSuffix(path, "/routes/subnets/reject") {
			machineActionsHandler.RejectSubnetRoute(w, r)
		} else {
			machinesHandler.Detail(w, r)
		}
	})
	mux.HandleFunc("/events", sseHandler.HandleSSE)
	mux.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			usersHandler.Create(w, r)
		} else {
			usersHandler.List(w, r)
		}
	})
	mux.HandleFunc("/users/", func(w http.ResponseWriter, r *http.Request) {
		// Handle different user actions based on URL path
		path := r.URL.Path
		if strings.HasSuffix(path, "/rename") {
			usersHandler.Rename(w, r)
		} else if strings.HasSuffix(path, "/delete") {
			usersHandler.Delete(w, r)
		} else if strings.HasSuffix(path, "/preauth-keys") {
			usersHandler.CreatePreAuthKey(w, r)
		} else {
			http.NotFound(w, r)
		}
	})
}
