
async function generateNGINXConf(conf = {}) {
    const { imposter, capacity, mode } = conf;

    //const instances = Array(parseInt(""+capacity)).fill();
    const { listContainerNames } = require("./containers");
    const instances = await listContainerNames(/^dynamic-load-balancer-mb-[0-9]+$/);

    const output = `
#####################
# instances.length = ${instances.length}
######################
worker_processes  32;
worker_rlimit_nofile 10000;

events {
    worker_connections  19000;
    use epoll;
    multi_accept on;
}

http {
    access_log off;
    sendfile on;
    tcp_nopush on;
    tcp_nodelay on;
    keepalive_timeout 5;
    keepalive_requests 10;
    lingering_close off;
    client_body_timeout 10s;
    client_header_timeout 10s;
    send_timeout 2s;
    client_max_body_size 200M;

    resolver 127.0.0.11 ipv6=off;

    upstream frontend {
        ${instances.map((name) => `
        server ${name}:9000 weight=1;`)
        .join("")}
    }

    ${instances.map((name) => `
    upstream ${name} {
        server ${name}:2525 weight=1;
    }`).join("")}

    server {
        listen 9000 ssl;
        
        ssl_certificate /conf/certs/cert.pem;
        ssl_certificate_key /conf/certs/key.pem;
        ssl_password_file /conf/certs/password.txt;

        error_page 497 =200 $request_uri;

        location /nginx_status {
            stub_status;
        }
        location / {
            proxy_http_version 1.1;
            proxy_set_header Connection "";
            proxy_pass http://frontend;
        }
    }
    server {
        listen 2525;
        
        location /nginx_status {
            stub_status;
        }
        ${instances.map((name) => `
        location /${name} {
            rewrite /${name}/(.*) /$1 break;
            proxy_pass http://${name};
        }`).join("")}
    }
}    `;

    return output;
}

module.exports = {
    generateNGINXConf,
}