package main

import (
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"go-balancer/config"
	"net/http"
	"sync"
	"time"
)

var logger *zap.Logger

// includes detail about state of each backend server
type ServerData struct {
	Connections int
	Alive       bool
}

// strategy interface
type Strategy interface {
	Route() string
}

// concrete
type RoundRobin struct {
	currentIndex int
	servers      map[string]*ServerData
	keys         []string
}

func (r *RoundRobin) Route() string {
	if len(r.keys) == 0 {
		return "No servers available"
	}

	// Iterate over servers based on the keys slice
	for {
		server := r.keys[r.currentIndex]
		r.currentIndex = (r.currentIndex + 1) % len(r.keys)

		if r.servers[server].Alive {
			return server
		}
	}
}

type LeastConnection struct {
	servers map[string]*ServerData
}

func (l *LeastConnection) Route() string {
	if len(l.servers) == 0 {
		return "No servers available"
	}

	// Find the server with the least connections that is also alive
	var leastConnServer string
	minConnections := int(^uint(0) >> 1) // maximum int value
	for server, data := range l.servers {
		if data.Alive && data.Connections < minConnections {
			leastConnServer = server
			minConnections = data.Connections
		}
	}

	// Update connections count for the selected server
	if leastConnServer != "" {
		l.servers[leastConnServer].Connections++
	}

	return leastConnServer
}

type Router struct {
	strategy Strategy
}

// SetStrategy sets the routing strategy
func (r *Router) SetStrategy(strategy Strategy) {
	r.strategy = strategy
}

// Route delegates the routing to the current strategy
func (r *Router) Route() string {
	if r.strategy == nil {
		return "No strategy set"
	}
	return r.strategy.Route()
}

type HealthCheck struct {
	serverData map[string]*ServerData
	interval   time.Duration
	timeout    time.Duration
	enabled    bool
}

// PerformCheck runs the health checks in a continuous loop
func (hc *HealthCheck) PerformCheck() {
	if !hc.enabled {
		return
	}

	logger.Info("Starting health check...")

	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			var wg sync.WaitGroup

			// Check each server concurrently
			for server := range hc.serverData {
				wg.Add(1)
				go func(server string) {
					defer wg.Done()
					alive := hc.checkServer(server)
					hc.serverData[server].Alive = alive
				}(server)
			}

			// Wait for all checks to complete
			wg.Wait()
		}
	}
}

// checkServer performs a single health check on the given server
func (hc *HealthCheck) checkServer(server string) bool {
	client := http.Client{
		Timeout: hc.timeout,
	}

	logger.Info("Checking server", zap.String("server", server))
	resp, err := client.Get(server)
	if err != nil {
		logger.Error("Server is down", zap.String("server", server), zap.Error(err))
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		logger.Info("Server is alive", zap.String("server", server))
		return true
	}

	logger.Info("Server returned status",
		zap.String("server", server),
		zap.Int("status_code", resp.StatusCode),
	)
	return false
}

func StartServer(address string, router *Router) error {
	// Define the request handler that will process incoming requests
	requestHandler := func(ctx *fasthttp.RequestCtx) {
		ReverseProxy(ctx, router)
	}

	logger.Info("Starting reverse proxy on", zap.String("address", address))
	return fasthttp.ListenAndServe(address, requestHandler)
}

// ReverseProxy handles an incoming request and forwards it to the backend server
func ReverseProxy(ctx *fasthttp.RequestCtx, router *Router) {
	backendServer := router.Route() // Get the backend server URL

	// Log the incoming request
	logger.Info("Received request", zap.String("method", string(ctx.Method())), zap.String("uri", string(ctx.RequestURI())))

	// Acquire a request object from the pool and copy the incoming request
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	ctx.Request.CopyTo(req)

	// Set the backend server URL with the original request URI
	req.SetRequestURI(backendServer + string(ctx.RequestURI()))

	// Log the proxying action
	logger.Info("Forwarding request to backend", zap.String("backend_server", backendServer), zap.String("full_request_uri", string(req.RequestURI())))

	// Acquire a response object to store the backend's response
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	// Forward the request to the backend server
	err := fasthttp.Do(req, resp)
	if err != nil {
		ctx.Error("Failed to reach backend", fasthttp.StatusBadGateway)
		logger.Error("Error proxying request", zap.Error(err), zap.String("backend_server", backendServer))
		return
	}

	// Log the response status code from the backend
	logger.Info("Received response from backend", zap.Int("status_code", resp.StatusCode()))

	// Forward response from backend back to client
	resp.CopyTo(&ctx.Response)
}

func main() {
	var err error // this for global logger, remove later
	logger, err = zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		logger.Fatal("Error loading config", zap.Error(err))
	}
	serverData := make(map[string]*ServerData)
    
	for _, serverURL := range cfg.Serverpool {
		serverData[serverURL] = &ServerData{
			Connections: 0,
			Alive:       true,
		}
	}

	router := &Router{}

	switch cfg.Routing.Strategy {
	case "round_robin":
		logger.Info("Using round-robin strategy.")
		roundRobinStrategy := &RoundRobin{
			currentIndex: 0,
			servers:      serverData,
			keys:         cfg.Serverpool,
		}
		router.SetStrategy(roundRobinStrategy)

	case "least_connection":
		logger.Info("Using least connection strategy.")
		leastConnectionStrategy := &LeastConnection{
			servers: serverData,
		}
		router.SetStrategy(leastConnectionStrategy)

	default:
		logger.Error("Unknown balancer strategy", zap.String("strategy", cfg.Routing.Strategy))
	}
    
	healthCheck := &HealthCheck{
		serverData: serverData,
		interval:   time.Duration(cfg.Healthcheck.Interval) * time.Second,
		timeout:    time.Duration(cfg.Healthcheck.Timeout) * time.Second,
		enabled:    cfg.Healthcheck.Enabled,
	}

	go healthCheck.PerformCheck()
    
	if err := StartServer(cfg.ProxyServer.Address, router); err != nil {
		logger.Fatal("Error starting server", zap.Error(err))
	}
}
