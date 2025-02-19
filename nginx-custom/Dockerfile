# syntax=docker/dockerfile:experimental
ARG NGINX_VERSION
FROM nginx:${NGINX_VERSION} as build

RUN apt-get update && \
    apt-get install -y \
        build-essential \
        openssh-client \
        git \
        wget \
        curl \
        libxml2 \
        libxslt1-dev \
        libpcre3 \
        libpcre3-dev \
        zlib1g \
        zlib1g-dev \
        openssl \
        libssl-dev \
        libpcre3-dev \
        libtool \
        automake \
        gcc \
        g++ \
        make \
        iputils-ping vim gnupg2 \
        ca-certificates lsb-release libssl-dev libgd-dev \
        libxml2 libxml2-dev uuid-dev \
        libhiredis-dev && \
    rm -rf /var/cache/apt
    
RUN wget "http://nginx.org/download/nginx-${NGINX_VERSION}.tar.gz" && \
    tar -C /usr/src -xzvf nginx-${NGINX_VERSION}.tar.gz

# RUN mkdir -p -m 0600 ~/.ssh && \
#     ssh-keyscan github.com >> ~/.ssh/known_hosts

# WORKDIR /src/ngx_devel_kit
# RUN --mount=type=ssh git clone git@github.com:simpl/ngx_devel_kit .

# WORKDIR /src/set-misc-nginx-module
# RUN --mount=type=ssh git clone git@github.com:openresty/set-misc-nginx-module.git .

WORKDIR /usr/src/nginx-${NGINX_VERSION}
RUN ./configure --prefix=/etc/nginx \
    --sbin-path=/usr/sbin/nginx  \
    --modules-path=/usr/lib/nginx/modules  \
    --conf-path=/etc/nginx/nginx.conf  \
    --error-log-path=/var/log/nginx/error.log  \
    --http-log-path=/var/log/nginx/access.log  \
    --pid-path=/var/run/nginx.pid  \
    --lock-path=/var/run/nginx.lock \
    --http-client-body-temp-path=/var/cache/nginx/client_temp \
    --http-proxy-temp-path=/var/cache/nginx/proxy_temp  \
    --http-fastcgi-temp-path=/var/cache/nginx/fastcgi_temp  \
    --http-uwsgi-temp-path=/var/cache/nginx/uwsgi_temp  \
    --http-scgi-temp-path=/var/cache/nginx/scgi_temp  \
    --user=nginx --group=nginx \
    --with-debug \
    --with-compat \
    --with-file-aio  \
    --with-threads  \
    --with-http_addition_module   \
    --with-http_dav_module  \
    --with-http_flv_module  \
    --with-http_gunzip_module  \
    --with-http_gzip_static_module  \
    --with-http_mp4_module  \
    --with-http_random_index_module \
    --with-http_realip_module  \
    --with-http_secure_link_module  \
    --with-http_slice_module  \
    --with-http_ssl_module  \
    --with-http_stub_status_module  \
    --with-http_sub_module  \
    --with-http_v2_module  \
    --with-mail  \
    --with-mail_ssl_module  \
    --with-stream  \
    --with-stream_realip_module  \
    --with-http_auth_request_module \
    --with-stream_ssl_module \
    --with-stream_ssl_preread_module  \
    --with-ld-opt='-Wl,-z,relro -Wl,-z,now -Wl,--as-needed -pie' && \
    make modules

ENV NGINX_VERSION=${NGINX_VERSION}
WORKDIR /tmp
# COPY install-nginx.sh .
# RUN chmod 777 install-nginx.sh

# RUN ./install-nginx.sh NGINX_VERSION=${NGINX_VERSION} NGINX_INSTALL_MODE=${NGINX_INSTALL_MODE}

COPY build-module.sh .
COPY module2 /tmp/module2

RUN chmod 777 build-module.sh
RUN ls -lh /tmp/module2 && \
    ./build-module.sh module2 && \
    ls /usr/src/nginx-${NGINX_VERSION}/objs

RUN rm -rf /tmp/*
    

FROM nginx:${NGINX_VERSION}

RUN apt-get update && \
    apt-get install -y \
    libhiredis-dev

COPY --from=build /etc/nginx/modules/ngx_http_custom_module.so /usr/lib/nginx/modules/

ENTRYPOINT [ "nginx" ]
CMD [ "-c","/nginx.conf","-g","daemon off;"]