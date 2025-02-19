# Usage Steps

````sh
docker build . -t airdk2/hipache-nginx --build-arg="NGINX_VERSION=1.25.2"

docker run --net=host -v %cd%/module2/nginx.conf:/nginx.conf --name hipache-nginx -d airdk2/hipache-nginx nginx -c /nginx.conf -g "daemon off;"
````

## To Compile Changes

````sh
# Build a development image
docker build . -t ngx-mod-dev-img --build-arg="NGINX_VERSION=1.25.2"

# Run the container with your local code mounted
docker run -it -d --net=host -v %cd%:/code --name ngx-mod-dev-container ngx-mod-dev-img:latest

# Exec a bash shell into the contianer 
docker exec -it ngx-mod-dev-container bash

# To compile latest changes run:
./build-module.sh nginx-custom/module2

# Test it out
nginx -c /code/nginx-custom/module2/nginx.conf

````