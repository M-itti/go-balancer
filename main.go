package main

import (
	"errors"
	"fmt"
	"github.com/valyala/fasthttp"
	"go-balancer/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"
)

type ServerData struct {
	Connections   int
	Alive         bool
	attempt       int
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
	mu           sync.Mutex
}

func (r *RoundRobin) Route() string {
	if len(r.keys) == 0 {
		return "No servers available"
	}

	// Iterate over servers based on the keys slice
	for {
		r.mu.Lock()
		server := r.keys[r.currentIndex]
		r.currentIndex = (r.currentIndex + 1) % len(r.keys)
		r.mu.Unlock()

		if r.servers[server].Alive {
			return server
		}
		return ""
	}
}

type LeastConnection struct {
	servers map[string]*ServerData
	mu      sync.RWMutex
}

func (l *LeastConnection) Route() string {
	if len(l.servers) == 0 {
		return "No servers available"
	}

	// Find the server with the least connections that is also alive
	var leastConnServer string
	minConnections := int(^uint(0) >> 1) // maximum int value

	l.mu.RLock()
	for server, data := range l.servers {
		if data.Alive && data.Connections < minConnections {
			leastConnServer = server
			minConnections = data.Connections
		}
	}
	l.mu.RUnlock()

	// Update connections count for the selected server
	if leastConnServer != "" {
		l.mu.Lock()
		l.servers[leastConnServer].Connections++
		l.mu.Unlock()
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
func (r *Router) Route() (string, error) {
	if r.strategy == nil {
		return "", errors.New("No strategy set")
	}
	return r.strategy.Route(), nil
}

type HealthCheck struct {
	interval       time.Duration
	initialBackoff time.Duration
	maxBackoff     time.Duration
	timeout        time.Duration
	serverPool     map[string]*ServerData
	mu             sync.Mutex
	enabled        bool
	logger         *zap.Logger
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
					hc.logger.Info("Server is alive", zap.String("server", server))
				} else {
					hc.logger.Warn("Server is down", zap.String("server", server))
					hc.mu.Lock()
					hc.serverPool[server].Alive = false
					hc.mu.Unlock()
					go hc.retry(server)
				}
			}
		}
		time.Sleep(hc.interval)
	}
}

func (hc *HealthCheck) retry(server string) {
	httpClient := &http.Client{}

	if hc.timeout > 0 {
		httpClient.Timeout = hc.timeout
	}
	attempt := 0

	for {
		hc.logger.Info("Retrying server",
			zap.String("server", server),
			zap.Int("attempt", attempt+1),
		)

		_, err := httpClient.Get(server)
		if err == nil {
			hc.mu.Lock()
			hc.serverPool[server].Alive = true
			hc.mu.Unlock()

			// Log successful reconnection
			hc.logger.Info("Server is back online", zap.String("server", server))
			break
		}

		hc.mu.Lock()
		hc.serverPool[server].Alive = false
		hc.mu.Unlock()

		backoffTime := hc.calculateBackoff(attempt)

		// Log failure to reach server with backoff information
		hc.logger.Warn("Failed to reach server, retrying",
			zap.String("server", server),
			zap.Duration("backoff", backoffTime),
		)

		time.Sleep(backoffTime)
		attempt++
	}
}

func StartServer(address string, router *Router, logger *zap.Logger) error {
	requestHandler := func(ctx *fasthttp.RequestCtx) {
		ReverseProxy(ctx, router, logger)
	}

	logger.Info("Starting reverse proxy on", zap.String("address", address))
	return fasthttp.ListenAndServe(address, requestHandler)
}

// ReverseProxy handles an incoming request and forwards it to the backend server
func ReverseProxy(ctx *fasthttp.RequestCtx, router *Router, logger *zap.Logger) {
	backendServer, err := router.Route() // Get the backend server URL
	if err != nil {
		ctx.Error("No backend server available", fasthttp.StatusServiceUnavailable)
		logger.Error("Error selecting backend server", zap.Error(err))
		return
	}

	logger.Info("Received request", zap.String("method", string(ctx.Method())), zap.String("uri", string(ctx.RequestURI())))

	// Acquire a request object from the pool and copy the incoming request
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	ctx.Request.CopyTo(req)

	// Set the backend server URL with the original request URI
	req.SetRequestURI(backendServer + string(ctx.RequestURI()))

	logger.Info("Forwarding request to backend", zap.String("backend_server", backendServer), zap.String("full_request_uri", string(req.RequestURI())))

	// Acquire a response object to store the backend's response
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	// Forward the request to the backend server
	err = fasthttp.Do(req, resp)
	if err != nil {
		ctx.Error("Failed to reach backend", fasthttp.StatusBadGateway)
		logger.Error("Error proxying request", zap.Error(err), zap.String("backend_server", backendServer))
		return
	}

	logger.Info("Received response from backend", zap.Int("status_code", resp.StatusCode()))

	// Forward response from backend back to client
	resp.CopyTo(&ctx.Response)
}

func NewHealthCheck(cfg *config.Config, serverData map[string]*ServerData) (*HealthCheck, error) {
	// TODO: hard coded
	healthLogFile, err := os.OpenFile("/tmp/healthcheck.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open healthcheck.log: %w", err)
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	healthCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.AddSync(healthLogFile),
		zap.InfoLevel,
	)
	healthLogger := zap.New(healthCore)

	return &HealthCheck{
		initialBackoff: time.Duration(cfg.Healthcheck.InitialBackoff) * time.Second,
		interval:       time.Duration(cfg.Healthcheck.Interval) * time.Second,
		maxBackoff:     time.Duration(cfg.Healthcheck.MaxBackoff) * time.Second,
		timeout:        time.Duration(cfg.Healthcheck.Timeout) * time.Second,
		enabled:        cfg.Healthcheck.Enabled,
		serverPool:     serverData,
		mu:             sync.Mutex{},
		logger:         healthLogger,
	}, nil
}

func main() {
    logger, err := zap.NewProduction()
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
			Connections:   0,
			Alive:         true,
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
			mu:           sync.Mutex{},
		}
		router.SetStrategy(roundRobinStrategy)

	case "least_connection":
		logger.Info("Using least connection strategy.")
		leastConnectionStrategy := &LeastConnection{
			servers: serverData,
			mu:      sync.RWMutex{},
		}
		router.SetStrategy(leastConnectionStrategy)

	default:
		logger.Error("Unknown balancer strategy", zap.String("strategy", cfg.Routing.Strategy))
	}

	hc, err := NewHealthCheck(cfg, serverData)
	if err != nil {
		panic(err)
	}

	if cfg.Healthcheck.Enabled {
		logger.Info("Starting Healthcheck")
		go hc.healthCheck()
	}

	if err := StartServer(cfg.ProxyServer.Address, router, logger); err != nil {
		logger.Fatal("Error starting server", zap.Error(err))
	}
}
