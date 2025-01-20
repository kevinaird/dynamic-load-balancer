
@REM BASELINE

@REM docker-compose -p dynamic-load-balancer -f ./test/docker-compose-baseline.yml up -d --wait

@REM set REDIS_HOST=
@REM set MASTER_NGINX=http://localhost
@REM node watcher/deploy.js ./test/imposters.json 10 virtual
@REM node watcher/nginx.js 10 test/nginx-baseline.conf

@REM docker restart dynamic-load-balancer-nginx-1

@REM timeout 5

docker run --name k6 --rm ^
 -v %cd%/test/script.js:/script.js ^
 --network host ^
 -it grafana/k6 run --vus 200 --duration 1m /script.js

@REM BENCHMARK

@REM docker-compose -p dynamic-load-balancer -f ./docker-compose.yml -f ./test/docker-compose-test.yml up -d --wait

@REM set REDIS_HOST=localhost
@REM set REDIS_PORT=6379
@REM set MASTER_NGINX=http://localhost
@REM node watcher/deploy.js ./test/imposters.json 10 virtual

@REM curl -vX POST http://localhost:2525/!dynamic-load-balancer-mb-1/imposters -d @test/imposters.json
@REM curl -vX POST http://localhost:2525/!dynamic-load-balancer-mb-2/imposters -d @test/imposters.json
@REM curl -vX POST http://localhost:2525/!dynamic-load-balancer-mb-3/imposters -d @test/imposters.json

@REM docker exec -it dynamic-load-balancer-redis-1 redis-cli rpush frontend:9000 http://dynamic-load-balancer-mb-1:9000
@REM docker exec -it dynamic-load-balancer-redis-1 redis-cli rpush frontend:9000 http://dynamic-load-balancer-mb-2:9000
@REM docker exec -it dynamic-load-balancer-redis-1 redis-cli rpush frontend:9000 http://dynamic-load-balancer-mb-3:9000
