package observ

import (
	"encoding/json"
	"net/http"

	"github.com/Aswanidev-vs/mana/auth"
)

// HealthConfig holds configuration for the health endpoint.
type HealthConfig struct {
	EnableAuth bool
	JWTAuth    *auth.JWTAuth
}

// HealthHandler returns an HTTP handler for /health.
func HealthHandler(cfg HealthConfig, roomCount, peerCount, sessionCount func() int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if cfg.EnableAuth {
			tokenStr := auth.ExtractToken(r)
			if tokenStr == "" {
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
				return
			}
			if _, err := cfg.JWTAuth.ValidateToken(tokenStr); err != nil {
				json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
				return
			}
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":   "ok",
			"rooms":    roomCount(),
			"peers":    peerCount(),
			"sessions": sessionCount(),
		})
	}
}

// MetricsConfig holds configuration for the metrics endpoint.
type MetricsConfig struct {
	EnableAuth bool
	JWTAuth    *auth.JWTAuth
}

// MetricsHandler returns an HTTP handler for /metrics.
func MetricsHandler(cfg MetricsConfig, metrics *Metrics, roomCount, peerCount, callCount func() int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.EnableAuth {
			tokenStr := auth.ExtractToken(r)
			if tokenStr == "" {
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			claims, err := cfg.JWTAuth.ValidateToken(tokenStr)
			if err != nil || claims.Role != "admin" {
				http.Error(w, "admin access required", http.StatusForbidden)
				return
			}
		}

		metrics.UpdateRooms(roomCount())
		metrics.UpdatePeerConnections(peerCount())
		metrics.UpdateCalls(callCount())

		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		w.Write([]byte(metrics.PrometheusText()))
	}
}
