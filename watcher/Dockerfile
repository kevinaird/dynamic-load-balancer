FROM mhart/alpine-node:16.4.2

RUN mkdir /app
WORKDIR /app
COPY package.json package-lock.json /app/

RUN npm install --production && npm cache clean -f

COPY . /app

CMD ["node","index.js"]