# Building the binary of the App
FROM golang:1.21 AS build

# `boilerplate` should be replaced with your project name
WORKDIR /go/src/stream_app

# Copy all the Code and stuff to compile everything
COPY . .

# Downloads all the dependencies in advance (could be left out, but it's more clear this way)
RUN go mod tidy

# Builds the application as a staticly linked one, to allow it to run on alpine
# ARM64 on t4g
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -o app .


# Moving the binary to the 'final Image' to make it smaller
FROM alpine:latest as release

WORKDIR /app


# `boilerplate` should be replaced here as well
COPY --from=build /go/src/stream_app/app .

ENV REDIS_HOST="" \
    REDIS_PORT="" \
    REDIS_PSW=""

# Add packages
RUN apk -U upgrade \
    && apk add --no-cache dumb-init ca-certificates \
    && chmod +x /app/app

# Exposes port 3000 because our program listens on that port
EXPOSE 3000

ENTRYPOINT ["/usr/bin/dumb-init", "--"]