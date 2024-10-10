
/* Main */
type LoadBalancer struct {
  *Config
  listen_port int
  host string
  server_pool []map
  *Router
  *HealthCheck
}

/* Config */
type Config struct {
}


/* Balancer */
type ReverseProxy struct {
}

type HealthCheck struct {
}


// strategy interface
type Strategy interface {
}
// concrete
type RoundRobin struct {
}
type LeastConnection struct {
}
// context
type Router struct {
}

func main () {
}
