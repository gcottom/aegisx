package routes

import (
	"net/http"
	"sync/atomic"
)

type RouterSwitcher struct {
	current atomic.Value // Stores the current http.Handler
}

// NewRouterSwitcher initializes with a default handler
func NewRouterSwitcher(initial http.Handler) *RouterSwitcher {
	rs := &RouterSwitcher{}
	rs.current.Store(initial)
	return rs
}

// ServeHTTP delegates to the current handler
func (rs *RouterSwitcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler := rs.current.Load().(http.Handler)
	handler.ServeHTTP(w, r)
}

// UpdateRouter replaces the current handler with a new one
func (rs *RouterSwitcher) UpdateRouter(newHandler http.Handler) {
	rs.current.Store(newHandler)
}
