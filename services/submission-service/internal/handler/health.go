package handler

import (
	"context"
	"net/http"
	"time"
)

// healthz is the liveness probe: the process is up and serving HTTP. It deliberately checks no
// dependencies, so a database outage makes the pod unready rather than restarting it in a loop.
func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz is the readiness probe: every dependency answers within two seconds.
func (h *Handler) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	checks := map[string]string{}
	status, overall := http.StatusOK, "ok"
	for _, c := range h.opts.Readiness {
		if err := c.Check(ctx); err != nil {
			checks[c.Name] = err.Error()
			status, overall = http.StatusServiceUnavailable, "unavailable"
			continue
		}
		checks[c.Name] = "ok"
	}
	writeJSON(w, status, map[string]any{"status": overall, "checks": checks})
}
