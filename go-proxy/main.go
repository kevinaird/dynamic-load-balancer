package main

import (
	"fmt"
	"log"
	"strings"
	"strconv"
	"context"
	"errors"
	"sync"

	"go-proxy/utils"
	
	"github.com/redis/go-redis/v9"
)

var rdb *redis.Client = redis.NewClient(&redis.Options{
	Addr:	  fmt.Sprintf("%s:%s", utils.Config("REDIS_HOST","localhost"), utils.Config("REDIS_PORT","6379")),
	Password: utils.Config("REDIS_PASS",""),
	DB:		  utils.Configi("REDIS_DB",0),
})


func GetFrontends(ctx context.Context) ([]int, error) {

	frontendKeys, err := rdb.Keys(ctx, "frontend:*").Result()
	if err != nil {
		return nil, err
	}

	frontendsLen := len(frontendKeys)
	frontends := make([]int, frontendsLen)

	for i, frontendKey := range frontendKeys {
		frontends[i] = -1

		keyPair := strings.Split(frontendKey,":")
		if (len(keyPair)!=2) {
			log.Println("Unexpected format for frontend key:", frontendKey)
			continue
		}

		port, err := strconv.Atoi(keyPair[1])
		if err != nil {
			log.Println("Error parsing frontend key:", frontendKey, err)
			continue
		}

		log.Println("GetFrontends - Got port:", port)
		frontends[i] = port
	}

	return frontends, nil
}

func getClusterMutex(port int) *sync.Mutex {
	if cluster_mu[port] == nil {
		cluster_mu[port] = &sync.Mutex{}
	}
	return cluster_mu[port]
}

func MakeCluster(ctx context.Context, port int) (*Cluster, error) {
	// mu := getClusterMutex(port)
	// mu.Lock()
	// defer mu.Unlock()
	
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
	// mu := getClusterMutex(port)
	// mu.Lock()
	// defer mu.Unlock()

	cluster := Clusters[port]
	if cluster == nil {
		log.Println("No cluster to remove on port",port)
		return errors.New("No cluster to remove on port")
	}

	cluster.Teardown()
	Clusters[port] = nil

	return nil
}

func ListenForNewClusters(wg sync.WaitGroup) {
	defer wg.Done()
	ctx := context.Background()

	log.Println("Subscribed to new_frontend...")

	subscriber := rdb.Subscribe(ctx, "new_frontend")

	for {
		msg, err := subscriber.ReceiveMessage(ctx)
		if err != nil {
			log.Println("Error receiving new_frontend message:",err)
			continue
		}
		
		log.Println("Received new_frontend message:",msg.Payload)

		port, err := strconv.Atoi(msg.Payload)
		if err != nil {
			log.Println("Error parsing new_frontend message:", err)
			continue
		}

		go MakeCluster(ctx, port)
	}

	log.Println("Unsubscribed from new_frontend...")
}

func ListenForDownClusters(wg sync.WaitGroup) {
	defer wg.Done()

	ctx := context.Background()

	log.Println("Subscribed to remove_frontend...")
	subscriber := rdb.Subscribe(ctx, "remove_frontend")

	for {
		msg, err := subscriber.ReceiveMessage(ctx)
		if err != nil {
			log.Println("Error receiving remove_frontend message:",err)
			continue
		}
		
		log.Println("Received remove_frontend message:",msg.Payload)

		port, err := strconv.Atoi(msg.Payload)
		if err != nil {
			log.Println("Error parsing remove_frontend message:", err)
			continue
		}

		go RemoveCluster(ctx, port)
	}

	log.Println("Unsubscribed from remove_frontend...")
}


func main() {
	ctx := context.Background()

	Clusters = make(map[int]*Cluster)
	cluster_mu = make(map[int]*sync.Mutex)

	// check redis for what clusters should be deployed on startup
	ports, err := GetFrontends(ctx)
	if err != nil {
		log.Println("Error getting frontends on startup!!")
		panic(err)
	}

	for _, port := range ports {
		if port == -1 {
			continue
		}

		getClusterMutex(port)
		_, err := MakeCluster(ctx, port)
		if err != nil {
			log.Println("Error making cluster on port",port,err)
		}
	}

	log.Println("Number of Clusters up =", len(Clusters))

	var wg sync.WaitGroup

	// subscribe to new cluster publishes from redis
	wg.Add(1)
	go ListenForNewClusters(wg)

	// subscribe to delete cluster requests from redis
	wg.Add(1)
	go ListenForDownClusters(wg)

	wg.Wait()
}