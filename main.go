package main
import (
	"log"
	"fmt"
	"github.com/valyala/fasthttp"
    "sync"
    "time"
    "net/http"
    "go_balancer/config"
)

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

// Route method for RoundRobin, implementing the Strategy interface
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

// Route method for LeastConnection, implementing the Strategy interface
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
    serverPool map[string]*ServerData
    interval   time.Duration
    timeout    time.Duration
    enabled    bool
}

// PerformCheck runs the health checks in a continuous loop
func (hc *HealthCheck) PerformCheck() {
    if !hc.enabled {
        return
    }

    log.Println("Starting health check...")

    ticker := time.NewTicker(hc.interval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            var wg sync.WaitGroup

            // Check each server concurrently
            for server := range hc.serverPool {
                wg.Add(1)
                go func(server string) {
                    defer wg.Done()
                    alive := hc.checkServer(server)
                    hc.serverPool[server].Alive = alive
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

    log.Printf("Checking server: %s", server)
    resp, err := client.Get(server)
    if err != nil {
        log.Printf("Server %s is down: %v", server, err)
        return false
    }
    defer resp.Body.Close()

    if resp.StatusCode == http.StatusOK {
        log.Printf("Server %s is alive", server)
        return true
    }

    log.Printf("Server %s returned status %d", server, resp.StatusCode)
    return false
}

/*
type LoadBalancer struct {
  *Router
  *HealthCheck
  *Config
}
*/

func StartServer(address string, router *Router) error {
	// Define the request handler that will process incoming requests
	requestHandler := func(ctx *fasthttp.RequestCtx) {
		ReverseProxy(ctx, router)
	}

	log.Printf("Starting reverse proxy on %s", address)
	return fasthttp.ListenAndServe(address, requestHandler)
}

// handles an incoming request and forwards it to the backend server
func ReverseProxy(ctx *fasthttp.RequestCtx, router *Router) {
    backend_server := router.Route() 

	// Acquire a request object from the pool and copy the incoming request
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	ctx.Request.CopyTo(req)

	// Set the backend server URL with the original request URI
	req.SetRequestURI(backend_server + string(ctx.RequestURI()))

	// Acquire a response object to store the backend's response
	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	// Forward the request to the backend server
	err := fasthttp.Do(req, resp)
	if err != nil {
		ctx.Error("Failed to reach backend", fasthttp.StatusBadGateway)
		log.Printf("Error proxying request: %v", err)
		return
	}

	// Forward response from backend back to client
	resp.CopyTo(&ctx.Response)
}

func main() {
    
    cfg, err := config.LoadConfig("config.yaml")
    if err != nil {
        log.Fatalf("error loading config %s", err)
    }
    serverPool := cfg.Serverpool
    balancerStrat := cfg.Routing.Strategy

	serverData := make(map[string]*ServerData)

	// Loop over the server pool and add each entry to the map
	for _, serverURL := range serverPool {
		serverData[serverURL] = &ServerData{
			Connections: 0,
			Alive:       true,
		}
	}

    serverKeys := []string{"http://localhost:2000", "http://localhost:3000", "http://localhost:4000"}
    router := &Router{}
    
    // TODO: switch to switch case
	if balancerStrat == "round_robin" {
		fmt.Println("Using round-robin strategy.")
        roundRobinStrategy := &RoundRobin{
            currentIndex: 0,
            servers:      serverData,
            keys: serverKeys,
        }
        router.SetStrategy(roundRobinStrategy)

	} else if balancerStrat == "least_connection" {
        fmt.Println("Using RoundRobin strategy:")
        // TODO: this is hard coded
        leastConnectionStrategy := &LeastConnection{
            servers: serverData,
        }
        router.SetStrategy(leastConnectionStrategy)
       } else {
        fmt.Printf("Unknown balancer strategy: %s\n", balancerStrat)
    }

    healthCheck := &HealthCheck{
        serverPool: serverData,
        interval:   10 * time.Second,
        timeout:    2 * time.Second,
        enabled:    true,
    }
    
    
    go healthCheck.PerformCheck()


	// Start the proxy server
	if err := StartServer(":8080", router); err != nil {
		log.Fatalf("Error starting server: %s", err)
	}
}

