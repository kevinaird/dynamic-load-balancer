FROM golang:1.23.4 AS build-stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY *.go ./
COPY utils ./utils

RUN CGO_ENABLED=0 GOOS=linux go build -o /go-hipache

# Deploy the application binary into a lean image
FROM alpine AS build-release-stage

RUN apk --no-cache add curl

WORKDIR /

COPY --from=build-stage /go-hipache /go-hipache

ENTRYPOINT ["/go-hipache"]