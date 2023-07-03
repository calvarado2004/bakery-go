module github.com/calvarado2004/bakery-go

go 1.20

require (
	github.com/gorilla/mux v1.8.0
	github.com/streadway/amqp v1.1.0
	google.golang.org/grpc v1.56.1
	google.golang.org/protobuf v1.31.0
)

require (
	github.com/golang/protobuf v1.5.3 // indirect
	golang.org/x/net v0.11.0 // indirect
	golang.org/x/sys v0.9.0 // indirect
	golang.org/x/text v0.10.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20230530153820-e85fd2cbaebc // indirect
)

replace github.com/calvarado2004/bakery-go/proto => ./proto
