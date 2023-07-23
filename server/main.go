package main

import (
	"database/sql"
	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	rabbitmq "github.com/streadway/amqp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	_ "github.com/jackc/pgconn"
	_ "github.com/jackc/pgx/v4"
	_ "github.com/jackc/pgx/v4/stdlib"
	log "github.com/sirupsen/logrus"
)

type RabbitMQBakery struct {
	RabbitmqConnection *rabbitmq.Connection
	RabbitmqChannel    *rabbitmq.Channel
	Config
	orders map[int]*OrderStatus
	mu     sync.Mutex
}

type OrderStatus struct {
	Ch      chan *pb.BreadResponse
	Status  string
	OrderId int
}

type MakeBreadServer struct {
	pb.MakeBreadServer
	RabbitMQBakery *RabbitMQBakery
}

type CheckInventoryServer struct {
	pb.CheckInventoryServer
	Config
	PgConn         *sql.DB
	RabbitMQBakery *RabbitMQBakery
}

type BuyBreadServer struct {
	pb.BuyBreadServer
	RabbitMQBakery *RabbitMQBakery
}

type RemoveOldBreadServer struct {
	pb.RemoveOldBreadServer
	RabbitMQBakery *RabbitMQBakery
}

type Config struct {
	Repo   data.Repository
	Client *http.Client
}

var gRPCAddress = os.Getenv("BAKERY_SERVICE_ADDR")

var activemqAddress = os.Getenv("RABBITMQ_SERVICE_ADDR")

var counts int64

var rabbitmqConnection *rabbitmq.Connection
var rabbitmqChannel *rabbitmq.Channel

func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Errorf("Failed to open database: %v", err)
		return nil, err
	}

	if err = db.Ping(); err != nil {
		log.Errorf("Failed to ping database: %v", err)
		return nil, err
	}

	return db, nil

}

func connectToDB() *sql.DB {
	dsn := os.Getenv("DSN")

	for {
		connection, err := openDB(dsn)
		if err != nil {
			log.Warningf("Error opening database: %s", err)
			counts++
		} else {
			log.Println("Connected to database")
			return connection
		}

		if counts > 10 {
			log.Errorf("Could not connect to database after 10 attempts: %v", err)
			return nil
		}

		log.Println("Retrying in 5 seconds")
		time.Sleep(5 * time.Second)
		continue

	}
}

func (app *Config) setupRepo(conn *sql.DB) {
	db := data.NewPostgresRepository(conn)
	app.Repo = db

}

func main() {

	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	listen, err := net.Listen("tcp", gRPCAddress)
	if err != nil {
		log.Fatalf("error listening: %s ", err)
	}

	log.Printf("Server listening on %v", gRPCAddress)

	pgConn := connectToDB()
	if pgConn == nil {
		log.Panic("Could not connect to database")
	}

	rabbitmqConnection, err = rabbitmq.Dial(activemqAddress)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}

	rabbitmqChannel, err = rabbitmqConnection.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}

	// Setup RabbitMQ Bakery struct
	rabbitMQBakery := &RabbitMQBakery{
		Config:             Config{},
		RabbitmqConnection: rabbitmqConnection,
		RabbitmqChannel:    rabbitmqChannel,
		orders:             make(map[int]*OrderStatus),
	}

	// Setup Postgres Repository for RabbitMQ Bakery
	rabbitMQBakery.setupRepo(pgConn)

	// Initialize RabbitMQ
	rabbitMQBakery.init()

	server := grpc.NewServer()

	checkInventoryServer := &CheckInventoryServer{
		RabbitMQBakery: rabbitMQBakery,
	}

	makeBreadServer := &MakeBreadServer{
		RabbitMQBakery: rabbitMQBakery,
	}

	buyBreadServer := &BuyBreadServer{
		RabbitMQBakery: rabbitMQBakery,
	}

	removeOldBreadServer := &RemoveOldBreadServer{
		RabbitMQBakery: rabbitMQBakery,
	}

	pb.RegisterCheckInventoryServer(server, checkInventoryServer)
	pb.RegisterMakeBreadServer(server, makeBreadServer)
	pb.RegisterBuyBreadServer(server, buyBreadServer)
	pb.RegisterRemoveOldBreadServer(server, removeOldBreadServer)

	// Register reflection service on gRPC server.
	reflection.Register(server)

	// Start Bakery Server
	rabbitMQBakery.BakeryServer(listen, server)

}

// BakeryServer Go functions to run in the background
func (rabbit *RabbitMQBakery) BakeryServer(listen net.Listener, server *grpc.Server) {

	// Check bread every 30 seconds in the background and publish to RabbitMQ message queue make-bread-order when needed
	go func() {
		log.Println("Starting to check bread")
		for {
			err := rabbit.checkBread()
			if err != nil {
				log.Errorf("Failed to check bread: %v", err)
			}
			time.Sleep(30 * time.Second)
		}
	}()

	// Consume from RabbitMQ message queue buy-bread-order and perform buy bread
	go func() {

		err := rabbit.performBuyBread()
		if err != nil {
			log.Errorf("Failed to perform buy bread (main): %v", err)
			return
		}
		log.Printf("Ouch! Something went wrong with buy bread, we got disconnected from RabbitMQ")

	}()

	// Start gRPC Server in the background
	go func() {

		// Start gRPC Server
		if err := server.Serve(listen); err != nil {
			log.Fatalf("Failed to serve gRPC server over %v", err)
		}

	}()

	select {}

}
