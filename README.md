# bakery-go

## Environment variables
```bash
export BAKERY_SERVICE_ADDR=localhost:50051
export ACTIVEMQ_SERVICE_ADDR=amqp://guest:guest@localhost:5672/
```

## Generate proto files
```bash
protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative proto/bread.proto
```