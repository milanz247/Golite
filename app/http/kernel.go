package http

import (
	"encoding/json"
	"golite/container"
	"net/http"
)

type Context struct {
	Writer    http.ResponseWriter
	Request   *http.Request
	Handlers  []HandlerFunc
	Index     int
	Container *container.Container
}

func (c *Context) Next() {
	c.Index++
	for c.Index < len(c.Handlers) {
		c.Handlers[c.Index](c)
		c.Index++
	}
}

func (c *Context) JSON(code int, obj interface{}) {
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(code)
	json.NewEncoder(c.Writer).Encode(obj)
}

type HandlerFunc func(*Context)

type Kernel struct {
	Container        *container.Container
	GlobalMiddleware []HandlerFunc
	routes           map[string]HandlerFunc
}

func NewKernel(c *container.Container) *Kernel {
	return &Kernel{
		Container:        c,
		GlobalMiddleware: []HandlerFunc{},
		routes:           make(map[string]HandlerFunc),
	}
}

// Global Middleware එකතු කිරීමට
func (k *Kernel) Use(middleware HandlerFunc) {
	k.GlobalMiddleware = append(k.GlobalMiddleware, middleware)
}

func (k *Kernel) GET(path string, handler HandlerFunc) {
	k.routes["GET-"+path] = handler
}

// Request එකක් එන හැම වෙලාවකම ක්‍රියාත්මක වන Handler එක
func (k *Kernel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + "-" + r.URL.Path

	ctx := &Context{
		Writer:    w,
		Request:   r,
		Handlers:  []HandlerFunc{},
		Index:     -1,
		Container: k.Container,
	}

	// 1. Global Middlewares එකතු කිරීම
	ctx.Handlers = append(ctx.Handlers, k.GlobalMiddleware...)

	// 2. Route Handler එක තිබේ නම් එය එකතු කිරීම
	if handler, ok := k.routes[key]; ok {
		ctx.Handlers = append(ctx.Handlers, handler)
	} else {
		ctx.Handlers = append(ctx.Handlers, func(c *Context) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "404 Not Found"})
		})
	}

	// Middleware Chain එක සහ Controller එක ක්‍රියාත්මක කිරීම
	ctx.Next()
}