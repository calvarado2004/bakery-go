# frontend Dockerfile

# Start from golang 1.25 base image
FROM --platform=linux/amd64 docker.io/golang:1.25 as builder

# Add Maintainer Info
LABEL maintainer="Carlos Alvarado carlos-alvarado@outlook.com>"

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

COPY proto ./proto

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Copy the source from the current directory to the Working Directory inside the container
COPY frontend .

COPY frontend/cmd/web/templates ./cmd/web/templates

# Build the Go app
RUN CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build -o main ./cmd/web/

RUN chmod +x /app/main

FROM --platform=linux/amd64 docker.io/alpine:latest

RUN mkdir /app

COPY --from=builder /app/main /app

COPY frontend/cmd/web/templates ./cmd/web/templates

CMD [ "/app/main"]