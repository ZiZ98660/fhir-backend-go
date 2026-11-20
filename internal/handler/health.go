package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// OPTIONAL ABSTRACTION: Health only needs Ping, so its dependency is this small
// interface rather than the entire pgx pool type. A pool and a test fake can both
// satisfy it. They only need a matching method, not an explicit implements marker.
type DatabasePinger interface{ Ping(context.Context) error }

// BEFORE (earlier inline health handler; inactive excerpt):
//
//	if pool == nil {
//	    w.WriteHeader(http.StatusInternalServerError)
//	    return
//	}
//
// // Otherwise report database connected.
//
// NOW: Ping actually checks connectivity with a two-second deadline; failure is
// 503. A non-nil pool can exist while PostgreSQL is unreachable, so the old check
// was misleading. It still does not prove every application operation will work.
// WHY A SEPARATE FUNCTION: this endpoint can be tested with a fake pinger.
// Health(db) constructs a handler; its returned closure pings on each request.
// Extracting the function/file was optional; doing a real check fixed the behavior.
func Health(db DatabasePinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		w.Header().Set("Content-Type", "application/json")
		if err := db.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "down", "database": "disconnected"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "up", "database": "connected"})
	}
}
