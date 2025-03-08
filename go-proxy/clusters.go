package main

import (
	"fmt"
    "net/http"
    "net/http/httputil"
    "net/url"
	"math/rand"
	"log"
	"context"
	"errors"
	"time"
	"sync"
	
	"github.com/redis/go-redis/v9"
)

type Cluster struct {
	Port int
	Backends []string
	Protocol string
	CacheEnabled bool
	Delay int
	
	context context.Context

	rdb *redis.Client
	mu sync.Mutex
	proxy *httputil.ReverseProxy
	server *http.Server
}

type ContextKey string
const BackendContextKey ContextKey = "backend"

var Clusters map[int]*Cluster
var cluster_mu map[int]*sync.Mutex

func (c *Cluster) SetRedisClient(client *redis.Client) {
	c.rdb = client
}

func (c *Cluster) SetContext(ctx context.Context) {
	c.context = ctx
}

// Load backends for this cluster from redis and grab configs
func (c *Cluster) Refresh() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	ctx := c.context

	frontend_path := fmt.Sprintf("frontend:%d",c.Port)
	log.Println("LRANGE:",frontend_path)
	backends, err := c.rdb.LRange(ctx, frontend_path, 0, -1).Result()
	if err != nil {
		log.Println("Error fetching backends:",err)
		return err
	}

	c.Backends = backends
	backends_count := len(c.Backends)
	log.Println("Backends for port:",c.Port,"are",c.Backends,"count=",backends_count)

	if c.server != nil {
		c.Teardown()
	}

	if backends_count == 0 {
		return errors.New("No backends")
	}

	// TODO - Load Protocol, CacheEnabled and Delay config from redis too

	// create or update proxy servers 
	// TODO - This probably should be abstracted based on protocol type

	c.proxy = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			backend, ok  := r.In.Context().Value(BackendContextKey).(string)
			if !ok {
				return
			}
			// backend := c.Backends[rand.Intn(backends_count)]
			// log.Println("Selected backend", backend)

			target, err := url.Parse(backend)
			if err != nil {
				return
			}

			r.SetURL(target)
			r.Out.Host = r.In.Host
		},
	}
	
	request_handler := func(w http.ResponseWriter, req *http.Request) {
		// c.proxy.ServeHTTP(w, req)
		backend := c.Backends[rand.Intn(len(c.Backends))]
		w.Header().Set("X-Backend", backend)
		
		ctx := context.WithValue(req.Context(), BackendContextKey, backend)
		c.proxy.ServeHTTP(w, req.WithContext(ctx))
	}
	

	mux := http.NewServeMux()
	// mux.Handle("/!metrics", promhttp.Handler())
    mux.HandleFunc("/", request_handler)

	portStr := fmt.Sprintf(":%d", c.Port)
	c.server = &http.Server{Addr: portStr, Handler: mux }

	go func() {
		log.Println("Starting cluster on port", c.Port)

		if err := c.server.ListenAndServe(); err != nil {
			log.Println("Cluster on port", c.Port, "reported error:", err)
		}
	}()

	return nil
}

func (c* Cluster) Teardown() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	log.Println("Tearing down on port:", c.Port)
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := c.server.Shutdown(ctx); err != nil {
		log.Println("Error shutting down existing server on port:", c.Port)
		return err
	}

	log.Println("Torn down on port:", c.Port)
	c.server = nil
	c.proxy = nil
	
	return nil
}