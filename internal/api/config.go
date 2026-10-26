package api

import (
	"maps"
	"net/http"
)

// ConfigResponse is what the frontend links with: the Kelos console's
// base URL per cluster name, where a run's Task is shown. Empty when no
// console is configured: runs then have no link.
type ConfigResponse struct {
	KelosConsoles map[string]string `json:"kelosConsoles"`
}

func (a *api) config(*http.Request) (any, error) {
	consoles := make(map[string]string, len(a.cfg.KelosConsoles))
	maps.Copy(consoles, a.cfg.KelosConsoles)
	return ConfigResponse{KelosConsoles: consoles}, nil
}
