package main

import (
	"fmt"
	"log"
	"strings"
	"strconv"
	"context"
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

		ClusterMutex(port) // just to make sure a lock exists for this port
		go MakeCluster(ctx, port, rdb)
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

	InitClusters()

	InitProtocolRegistry()
	RegisterProtocol("http", newHttpProtocol)
	RegisterProtocol("https", newHttpsProtocol)
	// RegisterProtocol("tcp", newTcpProtocol)
	// RegisterProtocol("ws", newWsProtocol)
	// RegisterProtocol("wss", newWssProtocol)

	// check redis for what clusters should be deployed on startup
	ports, err := GetFrontends(ctx)
	if err != nil {
		log.Println("Error getting frontends on startup!!")
		panic(err)
	}

	// deploy clusters to all detected ports
	for _, port := range ports {
		if port == -1 {
			continue
		}

		ClusterMutex(port) // just to make sure a lock exists for this port
		_, err := MakeCluster(ctx, port, rdb)
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