# Dynamic Load Balancer

- Dynamic NGINX configuration stored in redis
- Still heavily in development
- Inspired by the [hipache-nginx](https://github.com/samalba/hipache-nginx) project
- Uses the [openresty](https://openresty.org/en/) version of nginx inorder to use lua scripts for connecting nginx to redis


## Usage

1. Run `docker-compose up`
2. Launch redis-commander in browser at http://localhost:8081 or use redis-cli
3. Run the following redis commands:
    - `rpush frontend:9000 http://mb1:2525`
    - `rpush frontend:9000 http://mb2:2525`
    - `rpush frontend:9001 http://mb1:2525`
4. Now `curl http://localhost:9000` should randomly go to either mb1 or mb2. And `curl http://localhost:9001` should always go to mb3.

## TODOs

- Benchmarks vs a oob nginx instance
- Observability
- Support for HTTPS - DONE
- Support for TCP
- Distributed Support - Kubernetes poc
- Log Forwarding