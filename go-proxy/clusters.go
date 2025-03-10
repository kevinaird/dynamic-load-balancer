package main

import (
	"fmt"
	"log"
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/redis/go-redis/v9"
)

type ClusterProtocol interface {
	StartServer() error
	ShutdownServer() error
}

var protocol_registry map[string]func(c *Cluster) ClusterProtocol

func RegisterProtocol(name string, protocolFactory func(c *Cluster) ClusterProtocol) {
	protocol_registry[name] = protocolFactory
}

func InitProtocolRegistry() {
	protocol_registry = make(map[string]func(c *Cluster) ClusterProtocol)
}

type Cluster struct {
	Port 			int
	Backends 		[]string
	Protocol 		string 		`yaml:"protocol"`
	CacheEnabled 	bool 		`yaml:"cacheEnabled"`
	Delay 			int 		`yaml:"delay"`
	
	context context.Context

	rdb 			*redis.Client
	mu 				sync.Mutex // prevents parallel refreshes or teardowns
	protocolImpl 	ClusterProtocol
}

var Clusters map[int]*Cluster // registry of all clusters
var cluster_mu map[int]*sync.Mutex // for preventing parallel changes to the same port in the Clusters map

func InitClusters() {
	Clusters = make(map[int]*Cluster)
	cluster_mu = make(map[int]*sync.Mutex)
}

func ClusterMutex(port int) *sync.Mutex {
	if cluster_mu[port] == nil {
		cluster_mu[port] = &sync.Mutex{}
	}
	return cluster_mu[port]
}

func MakeCluster(ctx context.Context, port int, rdb *redis.Client) (*Cluster, error) {
	mu := ClusterMutex(port)
	mu.Lock()
	defer mu.Unlock()
	
	var cluster *Cluster

	if Clusters[port] != nil {
		log.Println("Updating Cluster on port=",port)
		cluster = Clusters[port]
		cluster.Teardown()
	} else {
		cluster = &Cluster{
			Port: port,
		}
		cluster.SetContext(ctx)
		cluster.SetRedisClient(rdb)
		
		log.Println("New Cluster on port=",port)
	}

	err := cluster.Refresh()
	if (err != nil) {
		return nil, err
	}

	Clusters[port] = cluster
	log.Println("Cluster Ready on port=",port)

	return Clusters[port], nil
}

func RemoveCluster(ctx context.Context, port int) (error) {
	mu := ClusterMutex(port)
	mu.Lock()
	defer mu.Unlock()

	cluster := Clusters[port]
	if cluster == nil {
		log.Println("No cluster to remove on port",port)
		return errors.New("No cluster to remove on port")
	}

	cluster.Teardown()
	Clusters[port] = nil

	return nil
}

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

	if backends_count == 0 {
		return errors.New("No backends")
	}

	// Set defaults
	c.Protocol = "http"
	c.Delay = -1
	c.CacheEnabled = false

	// Load config from redis
	config_path := fmt.Sprintf("frontend_config:%d",c.Port)
	log.Println("GET:",config_path)
	config, err := c.rdb.Get(ctx, config_path).Result()
	if err == nil {
		err := json.Unmarshal([]byte(config), c)
		if err != nil {
			log.Println("Error parsing configuration for port",c.Port,err)
		}
	}

	log.Println("Cluster on port",c.Port,"protocol =",c.Protocol)
	log.Println("Cluster on port",c.Port,"delay =",c.Delay)
	log.Println("Cluster on port",c.Port,"cache enabled =",c.CacheEnabled)

	// create or update proxy servers 
	protocolFactory, ok := protocol_registry[c.Protocol]
	if !ok || protocolFactory == nil {
		return errors.New("No protocol found")
	}

	c.protocolImpl = protocolFactory(c)
	if c.protocolImpl == nil {
		return errors.New("Error setting protocol implementation")
	}

	return c.protocolImpl.StartServer()
}

func (c* Cluster) Teardown() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.protocolImpl == nil {
		return errors.New("Teardown attempted on a cluster with no protocol implementation set")
	}

	return c.protocolImpl.ShutdownServer()
}