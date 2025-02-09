package main

import (
    "crypto/tls"
    "fmt"
	"net"
    "net/http"
    "net/http/httputil"
    "net/url"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"context"
	"time"
	
	"go-hipache/utils"

	"github.com/redis/go-redis/v9"
	"github.com/patrickmn/go-cache"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

var backend_cache *cache.Cache
var proxy *httputil.ReverseProxy

type ContextKey string
const BackendContextKey ContextKey = "backend"

func config(name string, default_val string) string {
	val := os.Getenv(name)
	if val == "" {
		return default_val
	}
	return val
}

func configi(name string, default_val int) int {
	val := os.Getenv(name)
	if val == "" {
		return default_val
	}
	ival, err := strconv.Atoi(val)
	if err != nil {
		panic(err)
	}
	return ival
}

func get_backends(port string) ([]string, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:	  fmt.Sprintf("%s:%s", config("REDIS_HOST","localhost"), config("REDIS_PORT","6379")),
		Password: config("REDIS_PASS",""),
		DB:		  configi("REDIS_DB",0),
	})

	ctx := context.Background()

	frontend_path := fmt.Sprintf("frontend:%s",port)
	backends, err := rdb.LRange(ctx, frontend_path, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	return backends, nil
}

func get_backends_cached(port string) ([]string, bool, error) {
	_backends, found := backend_cache.Get(port)
	if found {
		backends, ok := _backends.([]string)
		if ok {
			return backends, true, nil
		}
	}

	backends, err := get_backends(port)
	if err != nil {
		return nil, false, err
	}

	backend_cache.Set(port, backends, cache.DefaultExpiration)
	return backends, false, nil
}

func handle_error(w http.ResponseWriter, err error) {
	msg := fmt.Sprintf("Error: %s", err)
	http.Error(w, msg, http.StatusBadRequest)
}

func request_handler(w http.ResponseWriter, req *http.Request) {
	// host, port := splitHostPort(req.Host)
	host, port, err := net.SplitHostPort(req.Host);
	if err != nil {
		handle_error(w, err)
		return
	}

	var backends []string
	var cacheHit bool

	skipCache := req.Header.Get("X-Backend-Skip-Cache")
	if skipCache != "" {
		backends, err = get_backends(port)
		if err != nil {
			handle_error(w, err)
			return
		}
	} else {
		backends, cacheHit, err = get_backends_cached(port)
		if err != nil {
			handle_error(w, err)
			return
		}
	}
	
	backend := backends[rand.Intn(len(backends))]

	// Check for direct calls to a backend using the format /!backend/some/path
	if strings.HasPrefix(req.URL.Path,"/!") {
		p := indexAt(req.URL.Path,"/",2)
		if p == -1 { p = len(req.URL.Path) }

		backendCheck := req.URL.Path[2:p]
		foundBackend := findBackend(backends, backendCheck)

		if foundBackend != "" {
			backend = foundBackend
			req.URL.Path = req.URL.Path[len(backendCheck)+2:len(req.URL.Path)]

			w.Header().Set("X-Backend-Direct", "1")
		} 
	}

	cacheHitStatus := "MISS"
	if cacheHit {
		cacheHitStatus = "HIT"
	}

	w.Header().Set("X-Backend-Cache-Status", cacheHitStatus)
	w.Header().Set("X-Backend", backend)

	debug := req.Header.Get("X-Backend-Debug")
	if debug != "" {
		resBody := fmt.Sprintf("hello on host=%s port=%s path=%s backend=%s\n", host, port, req.URL.Path, backend)
    	fmt.Fprintf(w, resBody)
	}

	ctx := context.WithValue(req.Context(), BackendContextKey, backend)
	proxy.ServeHTTP(w, req.WithContext(ctx))
	
}

func indexAt(s, sep string, n int) int {
	idx := strings.Index(s[n:], sep)
	if idx > -1 {
		idx += n
	}
	return idx
}

func findBackend(backends []string, backend string) string {
	for i := 0; i < len(backends); i++ {
	   parsed, err := url.Parse(backends[i])
	   if err != nil {
		  return ""
	   }

	   if parsed.Host == backend {
		  return backends[i]
	   }
	}
	return ""
 }

func main() {
	portRangeStr := config("PORT_RANGE","2525,9000-9010")
	rangesStr := strings.Split(portRangeStr,",")
	rangesLen := len(rangesStr)
	ranges := make([]*utils.PortRange, rangesLen)

	for i, rangeStr := range rangesStr {
		ranges[i] = utils.ParsePortRangeOrDie(rangeStr)
	}

	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	proxy = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			backend, ok  := r.In.Context().Value(BackendContextKey).(string)
			if !ok {
				return
			}

			target, err := url.Parse(backend)
			if err != nil {
				return
			}

			r.SetURL(target)
			r.Out.Host = r.In.Host
		},
	}

	finish := make(chan bool)

	backend_cache = cache.New(5*time.Minute, 10*time.Minute)

	server := http.NewServeMux()
	server.Handle("/!metrics", promhttp.Handler())
    server.HandleFunc("/", request_handler)

	for _, r := range ranges {
		for port := r.Base; port < r.Base+r.Size; port++ {
			go func(port int) {
				portStr := fmt.Sprintf(":%d", port)
				fmt.Println("Starting server on port", port)

				http.ListenAndServe(portStr, server)
			}(port)
		}
	}

	<-finish
}