package proxy

import (
	"errors"
	"net/http"
	"strings"
)

// RouteInventory is compiled from the frozen API by the serial integrator.
// There is no wildcard catch-all and public admin routes are never permitted.
type Route struct{ Method, Template string }
type Inventoried struct {
	next   http.Handler
	routes []Route
	prefix string
}

func NewInventoried(next http.Handler, prefix string, routes []Route) (*Inventoried, error) {
	if next == nil || len(routes) == 0 || len(routes) > 256 || !strings.HasPrefix(prefix, "/") {
		return nil, errors.New("invalid media route inventory")
	}
	copyRoutes := append([]Route(nil), routes...)
	for _, r := range copyRoutes {
		if !strings.HasPrefix(r.Template, "/") || strings.ContainsAny(r.Template, "*?%") || strings.Contains(r.Template, "..") || strings.HasPrefix(r.Template, "/admin") || (r.Method != "GET" && r.Method != "POST" && r.Method != "PUT" && r.Method != "DELETE" && r.Method != "PATCH") {
			return nil, errors.New("invalid public route")
		}
	}
	return &Inventoried{next, copyRoutes, strings.TrimSuffix(prefix, "/")}, nil
}
func (i *Inventoried) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, i.prefix+"/") {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, i.prefix)
	adminPath := strings.ToLower(path)
	if adminPath == "/admin" || strings.HasPrefix(adminPath, "/admin/") {
		http.NotFound(w, r)
		return
	}
	if strings.ContainsAny(path, "%\\") || strings.Contains(path, "..") || len(path) > 2048 {
		http.NotFound(w, r)
		return
	}
	for _, route := range i.routes {
		if route.Method == r.Method && matchRoute(route.Template, path) {
			i.next.ServeHTTP(w, r)
			return
		}
	}
	http.NotFound(w, r)
}
func matchRoute(template, path string) bool {
	a, b := strings.Split(template, "/"), strings.Split(path, "/")
	if len(a) != len(b) {
		return false
	}
	for j := range a {
		if strings.HasPrefix(a[j], "{") && strings.HasSuffix(a[j], "}") {
			if len(b[j]) == 0 || len(b[j]) > 128 {
				return false
			}
			continue
		}
		if a[j] != b[j] {
			return false
		}
	}
	return true
}
