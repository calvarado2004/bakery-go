# maker/Dockerfile

# Start from the latest golang base image
FROM --platform=linux/amd64 golang:latest as builder

# Add Maintainer Info
LABEL maintainer="Carlos Alvarado carlos-alvarado@outlook.com>"

# Set the Current Working Directory inside the container
WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

RUN go get "calvarado2004/bakery-go/proto"

# Copy the source from the current directory to the Working Directory inside the container
COPY server .

# Build the Go app
RUN CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build -o main .

RUN chmod +x /app/main

FROM --platform=linux/amd64 alpine:latest

RUN mkdir /app

COPY --from=builder /app/main /app

EXPOSE 50051

CMD [ "/app/main"]