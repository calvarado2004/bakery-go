package main

import (
	"database/sql"
	"encoding/json"
	"github.com/calvarado2004/bakery-go/data"
	rabbitmq "github.com/streadway/amqp"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"log"
	"net/http"
	"os"
	"time"

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
			log.Println("Error opening database:", err)
			counts++
		} else {
			log.Println("Connected to database")
			return connection
		}

		if counts > 10 {
			log.Println(err)
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

	pgConn := connectToDB()
	if pgConn == nil {
		log.Panic("Could not connect to database")
	}

	err := listenForMakeBread(pgConn)
	if err != nil {
		return
	}

}

func init() {
	var err error
	rabbitmqConnection, err = rabbitmq.Dial(rabbitmqAddress)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}

	rabbitmqChannel, err = rabbitmqConnection.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
}

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

		err = data.NewPostgresRepository(pgConn).AdjustBreadQuantity(bread.ID, bread.Quantity)
		if err != nil {
			return err
		}

		log.Printf("Bread made succesfully: %s", d.Body)

		err = d.Ack(false)
		if err != nil {
			return status.Errorf(codes.Internal, "Failed to acknowledge message: %v", err)
		}

		time.Sleep(1 * time.Second)

	}

	return nil
}
