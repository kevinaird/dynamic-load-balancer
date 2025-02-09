FROM openresty/openresty

RUN apt-get update ; apt-get -y install curl

ADD ./certs /conf/certs
ADD ./hipache.lua /conf/hipache.lua
ADD ./nginx.conf /nginx.conf
WORKDIR  /

ENTRYPOINT [ "nginx" ]
CMD [ "-c","/nginx.conf","-g","daemon off;"]