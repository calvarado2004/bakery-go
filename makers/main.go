package main

import (
	"database/sql"
	"encoding/json"

	"github.com/calvarado2004/bakery-go/data"
	rabbitmq "github.com/rabbitmq/amqp091-go"

	"net/http"
	"os"
	"time"

	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	_ "github.com/jackc/pgconn"
	_ "github.com/jackc/pgx/v4"
	_ "github.com/jackc/pgx/v4/stdlib"
)

var rabbitmqAddress = os.Getenv("RABBITMQ_SERVICE_ADDR")

var counts int64

type Config struct {
	Repo   data.Repository
	Client *http.Client
}

var rabbitmqConnection *rabbitmq.Connection
var rabbitmqChannel *rabbitmq.Channel

func openDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Errorf("Failed to open database: %v", err)
		return nil, err
	}

	if err = db.Ping(); err != nil {
		return nil, err
	}

	return db, nil

}

func connectToDB() *sql.DB {
	dsn := os.Getenv("DSN")

	for {
		connection, err := openDB(dsn)
		if err != nil {
			log.Errorf("Error opening database: %s", err)
			counts++
		} else {
			log.Println("Connected to database")
			return connection
		}

		if counts > 10 {
			log.Errorf("Error opening database, max attempts reached: %s", err)
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
	startMakersService()
}

// startMakersService initializes the maker service.
// It connects to the database and starts listening for make bread orders.
func startMakersService() {
	log.SetFormatter(&log.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	pgConn := connectToDB()
	if pgConn == nil {
		log.Panic("Could not connect to database")
	}

	err := listenForMakeBread(pgConn)
	if err != nil {
		log.Fatalf("Failed to listen for make bread order messages: %v", err)
		return
	}

	// Close connection
	err = pgConn.Close()
	if err != nil {
		log.Fatalf("Failed to close database connection: %v", err)
	}
}

func init() {
	initializeRabbitMQ(rabbitmqAddress)
}

// initializeRabbitMQ establishes a connection to RabbitMQ and opens a channel.
// It sets the global rabbitmqConnection and rabbitmqChannel variables.
func initializeRabbitMQ(rabbitmqAddr string) {
	if rabbitmqAddr == "" {
		log.Warn("RABBITMQ_SERVICE_ADDR not set, skipping RabbitMQ initialization")
		return
	}
	var err error
	rabbitmqConnection, err = rabbitmq.Dial(rabbitmqAddr)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}

	rabbitmqChannel, err = rabbitmqConnection.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
}

// listenForMakeBread consumes messages from the make-bread-order queue and updates bread quantities.
func listenForMakeBread(pgConn *sql.DB) error {

	log.Println("Listening for make bread order messages...")

	breadsBought, err := rabbitmqChannel.Consume(
		"make-bread-order", // queue
		"",                 // consumer
		false,              // auto-ack
		false,              // exclusive
		false,              // no-local
		false,              // no-wait
		nil,                // args
	)
	if err != nil {
		return status.Errorf(codes.Internal, "Failed to consume from make bread order queue: %v", err)
	}

	for d := range breadsBought {

		log.Printf("Received a message: %s", d.Body)

		bread := &data.Bread{}
		err := json.Unmarshal(d.Body, bread)
		if err != nil {
			err := d.Nack(false, true)
			if err != nil {
				return err
			}
			return status.Errorf(codes.Internal, "Failed to unmarshal bread data: %v", err)
		}

		_, err = data.NewPostgresRepository(pgConn).AdjustBreadQuantity(bread.ID, bread.Quantity)
		if err != nil {
			return err
		}

		log.Printf("Bread made successfully: %s", d.Body)

		err = d.Ack(false)
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to acknowledge message: %v", err)
		}

		time.Sleep(1 * time.Second)

	}

	return nil
}
