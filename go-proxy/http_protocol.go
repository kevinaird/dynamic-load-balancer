package main

import (
	"bytes"
	"context"
	"crypto/x509"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
    "net/http"
    "net/http/httputil"
    "net/url"
	"strconv"
	"time"

	"go-proxy/utils"

    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/hashicorp/terraform-plugin-sdk/helper/hashcode"
    "github.com/patrickmn/go-cache"
	"github.com/smira/go-statsd"
)

type ContextKey string
const BackendContextKey ContextKey = "backend"

type HttpContext struct {
	backend string
	start time.Time
	hash string
	delay int
	cluster *Cluster
}

type HttpProtocol struct {
	ClusterProtocol

	name string
	cluster *Cluster
	isHTTPS bool
	proxy *httputil.ReverseProxy
	server *http.Server
}

func newHttpProtocol(c *Cluster) ClusterProtocol {
	return &HttpProtocol{
		cluster: c,
		isHTTPS: false,
		name: "HTTP Protocol Cluster",
	}
}

func newHttpsProtocol(c *Cluster) ClusterProtocol {
	return &HttpProtocol{
		cluster: c,
		isHTTPS: true,
		name: "HTTPS Protocol Cluster",
	}
}

type CachedResponse struct {
	StatusCode int
	Header http.Header
	Body string
	Trailer http.Header
	Delay int
}

func (p *HttpProtocol) StartServer() error {
	c := p.cluster

	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	proxy_cache := cache.New(time.Duration(c.CacheTTL), 10*time.Minute)

	delay_handler := func(hctx HttpContext) {
		elapsed := int(time.Since(hctx.start).Milliseconds())
		if elapsed < hctx.delay {
			d := time.Duration(hctx.delay - elapsed)
			// log.Println("delaying...",hctx.Delay - elapsed,"ms because of delay",hctx.Delay,"ms with",elapsed,"ms elapsed.")
			time.Sleep(d * time.Millisecond)
		}
	}

	request_handler := func(w http.ResponseWriter, req *http.Request) {

		hctx := HttpContext{
			start: time.Now(),
			delay: c.Delay,
			cluster: c,
		}

		tag := statsd.StringTag("uri", fmt.Sprintf("%s %s",req.Method,req.URL.String()))
		statsd_client.Incr(fmt.Sprintf("%s.requests", c.Name), 1, tag)
		defer func() {
			statsd_client.Incr(fmt.Sprintf("%s.responses", c.Name), 1, tag)
			statsd_client.PrecisionTiming(fmt.Sprintf("%s.response_time", c.Name), time.Since(hctx.start), tag)
		}()

		// If cache is enabled build a hash key out of request method, url and request body.
		if c.CacheEnabled {
			reqBody, err := io.ReadAll(req.Body)
			if err == nil {
				defer req.Body.Close()

				reqUrl := req.URL.String()
				method := req.Method

				hctx.hash = hashcode.Strings([]string{method, reqUrl, string(reqBody)})

				// Then check for a cache entry for this hash key
				if cachedValue, found := proxy_cache.Get(hctx.hash); found {
					
					// If cache entry is found reply to client with the response and
					// skip going to the reverse proxy.
					cachedResponse, ok := cachedValue.(CachedResponse)

					if ok {
						w.Header().Set("X-Dlb-Cache", "HIT")
						statsd_client.Incr(fmt.Sprintf("%s.cache_hit", c.Name), 1, tag)
						
						if cachedResponse.Delay > 0 {
							hctx.delay = cachedResponse.Delay
						}

						// Handle delay
						delay_handler(hctx)

						for name, values := range cachedResponse.Header {
							for _, value := range values {
								w.Header().Add(name,value)
							}
						}

						w.WriteHeader(cachedResponse.StatusCode)
						io.WriteString(w, cachedResponse.Body)
						
						for name, values := range cachedResponse.Trailer {
							for _, value := range values {
								w.Header().Set(name, value)
							}
						}

						return
					}

				}

				// reset req.Body so that it can be read again up the chain
				req.Body = io.NopCloser(bytes.NewReader(reqBody))	
			}

			w.Header().Set("X-Dlb-Cache", "MISS")
			statsd_client.Incr(fmt.Sprintf("%s.cache_miss", c.Name), 1, tag)
		}

		// Select a random backend from the list of backends stored for this cluster
		backend := c.Backends[rand.Intn(len(c.Backends))]
		w.Header().Set("X-Dlb-Backend", backend)
		hctx.backend = backend
		
		// Forward the request to the reverse proxy
		ctx := context.WithValue(req.Context(), BackendContextKey, hctx)
		p.proxy.ServeHTTP(w, req.WithContext(ctx))
	}
	
	p.proxy = &httputil.ReverseProxy{
		Transport: NewTimingRoundtripper(http.DefaultTransport),
		// Forward the request to the backend selected in request_handler
		Rewrite: func(r *httputil.ProxyRequest) {
			hctx, ok  := r.In.Context().Value(BackendContextKey).(HttpContext)
			if !ok {
				return
			}

			target, err := url.Parse(hctx.backend)
			if err != nil {
				return
			}

			r.SetURL(target)
			r.Out.Host = r.In.Host
		},
		ModifyResponse: func(r *http.Response) error {
			hctx, ok := r.Request.Context().Value(BackendContextKey).(HttpContext)
			if !ok {
				return nil
			}

			// Check response for x-sv-delay response header
			svDelayStr := r.Header.Get("x-sv-delay")
			if svDelayStr != "" {
				svDelay, err := strconv.Atoi(svDelayStr)
				if err == nil {
					hctx.delay = svDelay
				}
			}

			// Implement response delay
			r.Header.Set("X-Dlb-Delay", fmt.Sprintf("%d",hctx.delay))
			delay_handler(hctx)

			// If cache is enabled save the response to cache
			if c.CacheEnabled {

				// Save to cache
				resBody, err := io.ReadAll(r.Body)
				if err == nil {
					defer r.Body.Close()

					responseToCache := CachedResponse{
						StatusCode: r.StatusCode,
						Header: r.Header,
						Body: string(resBody[:]),
						Trailer: r.Trailer,
						Delay: hctx.delay,
					}

					proxy_cache.Set(hctx.hash, responseToCache, cache.DefaultExpiration)

					// reset r.Body so that it can be read again up the chain
					r.Body = io.NopCloser(bytes.NewReader(resBody))	
				}
			}
			return nil
		},
	}
	
	mux := http.NewServeMux()
	mux.Handle("/!metrics", promhttp.Handler())
    mux.HandleFunc("/", request_handler)

	portStr := fmt.Sprintf(":%d", c.Port)

	if p.isHTTPS && c.MutualAuth {
		certFile := utils.Config("CERT_FILE","cert.pem")
		caCertFile := utils.Config("CA_CERT_FILE",certFile)

		caCert, _ := ioutil.ReadFile(caCertFile)
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(caCert)
		
		tlsConfig := &tls.Config{
			ClientCAs: caCertPool,
			ClientAuth: tls.RequireAnyClientCert,
		}
		tlsConfig.BuildNameToCertificate()
		
		p.server = &http.Server{
			Addr:      	portStr,
			TLSConfig: 	tlsConfig,
			Handler: 	mux,
		}
	} else {
		p.server = &http.Server{
			Addr: 		portStr, 
			Handler: 	mux,
		}
	}
	
	// Start the server in its own go-routine
	go func() {
		log.Println(p.name, "listening on port:", c.Port)

		if p.isHTTPS {
			certFile := utils.Config("CERT_FILE","cert.pem")
			keyFile := utils.Config("KEY_FILE","key.pem")

			if err := p.server.ListenAndServeTLS(certFile,keyFile); err != nil {
				log.Println(p.name, "on port", c.Port, "reported error:", err)
			}
		} else {
			if err := p.server.ListenAndServe(); err != nil {
				log.Println(p.name, "on port", c.Port, "reported error:", err)
			}
		}

	}()

	return nil
}

func (p *HttpProtocol) ShutdownServer() error {
	c := p.cluster
	
	log.Println("Tearing down",p.name,"on port:", c.Port)
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := p.server.Shutdown(ctx); err != nil {
		log.Println("Error shutting down existing",p.name,"on port:", c.Port)
		return err
	}

	log.Println("Torn down",p.name,"on port:", c.Port)
	p.server = nil
	p.proxy = nil

	return nil
}

type TimingRoundtripper struct {
    transport http.RoundTripper
}

func NewTimingRoundtripper(transport http.RoundTripper) http.RoundTripper {
    return TimingRoundtripper{transport: transport}
}

func (rt TimingRoundtripper) RoundTrip(r *http.Request) (resp *http.Response, err error) {
	name := "unknown-sim"
	hctx, ok := r.Context().Value(BackendContextKey).(HttpContext)
	if ok {
		name = hctx.cluster.Name
	}

	tag := statsd.StringTag("uri", fmt.Sprintf("%s %s",r.Method,r.URL.String()))

    start := time.Now()
    resp, err = rt.transport.RoundTrip(r)
	statsd_client.PrecisionTiming(fmt.Sprintf("%s.backend_time", name), time.Since(start), tag)
    return resp, err
}