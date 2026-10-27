package provider

import (
	"context"
	"strings"
)

// Route sends each model whose name starts with Prefix to Provider.
type Route struct {
	Prefix   string
	Provider Provider
}

// Router is a Provider that picks the provider for each request by its model
// name. The first route whose prefix matches wins. A model that matches no
// route goes to the fallback.
type Router struct {
	routes   []Route
	fallback Provider
}

var _ Provider = (*Router)(nil)

// NewRouter returns a Router that tries routes in order and uses fallback when
// no route matches.
func NewRouter(fallback Provider, routes ...Route) *Router {
	return &Router{routes: routes, fallback: fallback}
}

func (r *Router) ChatCompletion(ctx context.Context, req Request) (*Response, error) {
	return r.pick(req.Model).ChatCompletion(ctx, req)
}

func (r *Router) pick(model string) Provider {
	for _, route := range r.routes {
		if strings.HasPrefix(model, route.Prefix) {
			return route.Provider
		}
	}
	return r.fallback
}
