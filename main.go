package main

import (
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
	"go-balancer/config"
	"net/http"
	"sync"
	"time"
	"fmt"
	"math"
	"math/rand"
)

var logger *zap.Logger
var mu sync.Mutex

type ServerData struct {
	Connections int
	Alive       bool
    attempt     int
    checkInterval time.Duration
    lastCheckTime time.Time
}

type Strategy interface {
	Route() string
}

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
        mu.Lock()
		server := r.keys[r.currentIndex]
		r.currentIndex = (r.currentIndex + 1) % len(r.keys)
        mu.Unlock()

		if r.servers[server].Alive {
			return server
		}
        return ""
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

// sets the routing strategy
func (r *Router) SetStrategy(strategy Strategy) {
	r.strategy = strategy
}

// delegates the routing to the current strategy
func (r *Router) Route() string {
	if r.strategy == nil {
		return "No strategy set"
	}
	return r.strategy.Route()
}

type HealthCheck struct {
	interval       time.Duration
	initialBackoff time.Duration
	maxBackoff     time.Duration
    timeout        time.Duration
	serverPool     map[string]*ServerData
    mu             sync.Mutex
    enabled        bool
}

func (hc *HealthCheck) retry(server string) {
    httpClient := &http.Client{}

    if hc.timeout > 0 { 
        httpClient.Timeout = hc.timeout
    }
	attempt := 0

	for {
		fmt.Printf("Retrying server: %s, attempt: %d\n", server, attempt+1)
		_, err := http.Get(server)
		if err == nil {
			hc.mu.Lock()
			hc.serverPool[server].Alive = true
			hc.mu.Unlock()
			fmt.Printf("Server %s is back online\n", server)
			break
		}
		hc.mu.Lock()
		hc.serverPool[server].Alive = false
		hc.mu.Unlock()
		backoffTime := hc.calculateBackoff(attempt)
		fmt.Printf("Failed to reach %s. Waiting for %.2f seconds before next retry...\n", server, backoffTime.Seconds())
		time.Sleep(backoffTime)
		attempt++
	}
}

func (hc *HealthCheck) calculateBackoff(attempt int) time.Duration {
	backoffTime := float64(hc.initialBackoff) * math.Pow(2, float64(attempt))
	jitter := rand.Float64() * float64(hc.initialBackoff)
	totalSleepTime := time.Duration(math.Min(backoffTime, float64(hc.maxBackoff)) + jitter)
	return totalSleepTime
}

func (hc *HealthCheck) healthCheck() {
	for {
		for server := range hc.serverPool {
			hc.mu.Lock()
			alive := hc.serverPool[server].Alive
			hc.mu.Unlock()
			if alive {
				_, err := http.Get(server)
				if err == nil {
					fmt.Printf("Server %s is alive.\n", server)
				} else {
					fmt.Printf("Server %s is down.\n", server)
					hc.mu.Lock()
					hc.serverPool[server].Alive = false
					hc.mu.Unlock()
					go hc.retry(server)
				}
			}
		}
		fmt.Println()
		time.Sleep(hc.interval)
	}
}

func StartServer(address string, router *Router) error {
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
			checkInterval: time.Duration(cfg.Healthcheck.Interval) * time.Second,
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
    
	hc := &HealthCheck{
		initialBackoff: time.Duration(cfg.Healthcheck.InitialBackoff) * time.Second,
		interval:       time.Duration(cfg.Healthcheck.Interval) * time.Second,
		maxBackoff:     time.Duration(cfg.Healthcheck.MaxBackoff) * time.Second,
        timeout:        time.Duration(cfg.Healthcheck.Timeout) * time.Second, 
		enabled:        cfg.Healthcheck.Enabled,
        serverPool:     serverData, 
        mu:             sync.Mutex{}, 
	}

    if cfg.Healthcheck.Enabled {
        logger.Info("Starting Healthcheck")
        go hc.healthCheck()
    }
    
	if err := StartServer(cfg.ProxyServer.Address, router); err != nil {
		logger.Fatal("Error starting server", zap.Error(err))
	}
}
