package main

import (
	"context"
	// "encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
    "net/http"
    "net/http/httputil"
    "net/url"
	"time"

	"go-proxy/utils"

    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/hashicorp/terraform-plugin-sdk/helper/hashcode"
    "github.com/patrickmn/go-cache"
)

type ContextKey string
const BackendContextKey ContextKey = "backend"

type HttpContext struct {
	backend string
	start time.Time
	hash string
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
}

func (p *HttpProtocol) StartServer() error {
	c := p.cluster

	proxy_cache := cache.New(5*time.Minute, 10*time.Minute)

	delay_handler := func(ctx HttpContext) {
		elapsed := int(time.Since(ctx.start).Milliseconds())
		if elapsed < c.Delay {
			d := time.Duration(c.Delay - elapsed)
			// log.Println("delaying...",c.Delay - elapsed,"ms because of delay",c.Delay,"ms with",elapsed,"ms elapsed.")
			time.Sleep(d * time.Millisecond)
		}
	}

	request_handler := func(w http.ResponseWriter, req *http.Request) {

		hctx := HttpContext{
			start: time.Now(),
		}

		if c.CacheEnabled {
			// Check the cache
			// reqBody, err := ioutil.ReadAll(req.Body)
			// if err == nil {
			// 	err = req.Body.Close()
			// 	if err == nil {
					reqUrl := req.URL.String()
					method := req.Method

					hctx.hash = hashcode.Strings([]string{method, reqUrl}) //, string(reqBody)})

					if cachedValue, found := proxy_cache.Get(hctx.hash); found {
						// Send response

						cachedResponse, ok := cachedValue.(CachedResponse)
						// cachedResponse := CachedResponse{}
						// err := json.Unmarshal([]byte(cachedValue), cachedResponse)
						if ok {
							log.Println("Cache hit!")
							w.Header().Set("X-Cache", "HIT")
							
							// Handle delay
							delay_handler(hctx)

							for name, values := range cachedResponse.Header {
								// Loop over all values for the name.
								for _, value := range values {
									w.Header().Add(name,value)
								}
							}

							w.WriteHeader(cachedResponse.StatusCode)
							io.WriteString(w, cachedResponse.Body)
							
							for name, values := range cachedResponse.Trailer {
								// Loop over all values for the name.
								for _, value := range values {
									w.Header().Set(name, value)
								}
							}

							return
						}

					}
			// 	}
			// }

			log.Println("Cache miss!")
			w.Header().Set("X-Cache", "MISS")
		}

		backend := c.Backends[rand.Intn(len(c.Backends))]
		w.Header().Set("X-Backend", backend)
		hctx.backend = backend
		
		ctx := context.WithValue(req.Context(), BackendContextKey, hctx)
		p.proxy.ServeHTTP(w, req.WithContext(ctx))
	}
	
	p.proxy = &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			ctx, ok  := r.In.Context().Value(BackendContextKey).(HttpContext)
			if !ok {
				return
			}

			target, err := url.Parse(ctx.backend)
			if err != nil {
				return
			}

			r.SetURL(target)
			r.Out.Host = r.In.Host
		},
		ModifyResponse: func(r *http.Response) error {
			// TODO - Look for x-sv-delay header?
			if c.Delay > 0 {
				ctx, ok := r.Request.Context().Value(BackendContextKey).(HttpContext)
				if !ok {
					return nil
				}

				delay_handler(ctx)
			}
			if c.CacheEnabled {
				ctx, ok := r.Request.Context().Value(BackendContextKey).(HttpContext)
				if !ok {
					return nil
				}
				
				// Save to cache
				log.Println("Saving to cache!")
				resBody, err := ioutil.ReadAll(r.Body)
				if err == nil {
					err = r.Body.Close()
					if err == nil {
						responseToCache := CachedResponse{
							StatusCode: r.StatusCode,
							Header: r.Header,
							Body: string(resBody[:]),
							Trailer: r.Trailer,
						}

						proxy_cache.Set(ctx.hash, responseToCache, cache.DefaultExpiration)
						
						// responseToCacheString, err := json.Marshal(responseToCache)
						// if err == nil {
						// 	proxy_cache.Set(hash, string(resBody), cache.DefaultExpiration)
						// }
						
					}
				}
			}
			return nil
		},
	}
	
	mux := http.NewServeMux()
	mux.Handle("/!metrics", promhttp.Handler())
    mux.HandleFunc("/", request_handler)

	portStr := fmt.Sprintf(":%d", c.Port)
	p.server = &http.Server{Addr: portStr, Handler: mux }

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