package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// guard wraps next with the checks every request passes, in this order:
//
//  1. Host must be localhost, 127.0.0.1 or [::1] with the server's port (421
//     otherwise), which stops DNS rebinding.
//  2. An Origin header, when present, must name one of those hosts (403).
//  3. A request that changes anything must carry "X-Waypoint: 1" (403). A
//     browser cannot add that header to a cross-origin request without a
//     preflight, and the server never answers preflights.
//
// A panic below is logged and answered with a generic 500.
func (a *api) guard(next http.Handler) http.Handler {
	port := strconv.Itoa(a.d.Port)
	allowed := map[string]bool{
		"localhost:" + port: true,
		"127.0.0.1:" + port: true,
		"[::1]:" + port:     true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				a.log.Error("panic in handler", "method", r.Method, "path", r.URL.Path, "panic", v)
				a.writeJSON(w, r, http.StatusInternalServerError, errorBody{Error: "internal error", Code: "error"})
			}
		}()

		if !allowed[strings.ToLower(r.Host)] {
			a.writeJSON(w, r, http.StatusMisdirectedRequest, errorBody{Error: "Waypoint only answers on localhost", Code: "misdirected"})
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			u, err := url.Parse(o)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || !allowed[strings.ToLower(u.Host)] {
				a.writeJSON(w, r, http.StatusForbidden, errorBody{Error: "cross-origin requests are not allowed", Code: "forbidden"})
				return
			}
		}
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			if r.Header.Get("X-Waypoint") != "1" {
				a.writeJSON(w, r, http.StatusForbidden, errorBody{Error: `missing header "X-Waypoint: 1"`, Code: "forbidden"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
