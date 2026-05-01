module github.com/calvarado2004/bakery-go

go 1.26

require (
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/gorilla/csrf v1.7.3
	github.com/gorilla/mux v1.8.1
	github.com/jackc/pgconn v1.14.3
	github.com/jackc/pgx/v4 v4.18.3
	github.com/rabbitmq/amqp091-go v1.10.0
	github.com/sirupsen/logrus v1.9.4
	golang.org/x/crypto v0.49.0
	google.golang.org/grpc v1.80.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/gorilla/securecookie v1.1.2 // indirect
	github.com/jackc/chunkreader/v2 v2.0.1 // indirect
	github.com/jackc/pgio v1.0.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgproto3/v2 v2.3.3 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgtype v1.14.4 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.35.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260406210006-6f92a3bedf2d // indirect
)

replace github.com/calvarado2004/bakery-go/proto => ./proto

replace github.com/calvarado2004/bakery-go/data => ./data
