# broker Dockerfile

# Start from the latest golang base image
FROM --platform=linux/amd64 docker.io/golang:latest as builder

# Add Maintainer Info
LABEL maintainer="Carlos Alvarado carlos-alvarado@outlook.com>"

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

COPY proto ./proto

COPY data ./data

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed

RUN  go get github.com/sirupsen/logrus; go get github.com/google/uuid; go get github.com/jackc/pgconn; go get github.com/jackc/pgx/v4; go get github.com/calvarado2004/bakery-go/proto; go get github.com/calvarado2004/bakery-go/data; go get github.com/rabbitmq/amqp091-go; go get google.golang.org/grpc; go get google.golang.org/grpc/codes; go get google.golang.org/grpc/reflection;  go get google.golang.org/grpc/status; go mod download

# Copy the source from the current directory to the Working Directory inside the container
COPY broker .

# Build the Go app
RUN CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build -o main .

RUN chmod +x /app/main

FROM --platform=linux/amd64 docker.io/alpine:latest

RUN mkdir /app

COPY --from=builder /app/main /app

CMD [ "/app/main"]