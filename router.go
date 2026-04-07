package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type Route struct {
	Method  string
	Path    string
	Handler http.HandlerFunc
}

type Router struct {
	routes []Route
}

func NewRouter() *Router {
	return &Router{}
}

func (r *Router) Handle(method, path string, handler http.HandlerFunc) {
	r.routes = append(r.routes, Route{method, path, handler})
}

func (r *Router) GET(path string, handler http.HandlerFunc) {
	r.Handle("GET", path, handler)
}

func (r *Router) POST(path string, handler http.HandlerFunc) {
	r.Handle("POST", path, handler)
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	for _, route := range r.routes {
		if route.Method == req.Method && matchPath(route.Path, req.URL.Path) {
			route.Handler(w, req)
			return
		}
	}
	http.Error(w, "not found", 404)
}

func matchPath(pattern, path string) bool {
	if pattern == path {
		return true
	}
	parts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")
	if len(parts) != len(pathParts) {
		return false
	}
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			continue
		}
		if part != pathParts[i] {
			return false
		}
	}
	return true
}

func jsonResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func parseBody(req *http.Request, v interface{}) error {
	defer req.Body.Close()
	return json.NewDecoder(req.Body).Decode(v)
}

func getPathParam(pattern, path string, name string) string {
	parts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")
	for i, part := range parts {
		if part == ":"+name && i < len(pathParts) {
			return pathParts[i]
		}
	}
	return ""
}

func notImplemented(w http.ResponseWriter, r *http.Request) {
	http.Error(w, fmt.Sprintf("%s %s not implemented", r.Method, r.URL.Path), 501)
}

func (r *Router) DELETE(path string, handler http.HandlerFunc) {
	r.Handle("DELETE", path, handler)
}

func (r *Router) PUT(path string, handler http.HandlerFunc) {
	r.Handle("PUT", path, handler)
}

func (r *Router) Use(middleware func(http.HandlerFunc) http.HandlerFunc) {
	for i, route := range r.routes {
		r.routes[i].Handler = middleware(route.Handler)
	}
}

func (r *Router) ListRoutes() []string {
	var routes []string
	for _, route := range r.routes {
		routes = append(routes, route.Method+" "+route.Path)
	}
	return routes
}

func (r *Router) Group(prefix string) *Router {
	sub := NewRouter()
	sub.routes = r.routes
	return sub
}

func extractParams(pattern, path string) map[string]string {
	params := map[string]string{}
	parts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") && i < len(pathParts) {
			params[part[1:]] = pathParts[i]
		}
	}
	return params
}
