package main

import (
	"database/sql"
	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	_ "github.com/jackc/pgconn"
	_ "github.com/jackc/pgx/v4"
	_ "github.com/jackc/pgx/v4/stdlib"
)

type MakeBreadServer struct {
	pb.MakeBreadServer
}

type CheckInventoryServer struct {
	pb.CheckInventoryServer
}

type BuyBreadServer struct {
	pb.BuyBreadServer
}

type RemoveOldBreadServer struct {
	pb.RemoveOldBreadServer
}

type Config struct {
	Repo   data.Repository
	Client *http.Client
}

var gRPCAddress = os.Getenv("BAKERY_SERVICE_ADDR")

var activemqAddress = os.Getenv("RABBITMQ_SERVICE_ADDR")

var counts int64

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

	listen, err := net.Listen("tcp", gRPCAddress)
	if err != nil {
		log.Print("error listening: ", err)
	}

	log.Printf("Server listening on %v", gRPCAddress)

	server := grpc.NewServer()
	pb.RegisterMakeBreadServer(server, &MakeBreadServer{})
	pb.RegisterBuyBreadServer(server, &BuyBreadServer{})
	pb.RegisterCheckInventoryServer(server, &CheckInventoryServer{})
	pb.RegisterRemoveOldBreadServer(server, &RemoveOldBreadServer{})

	reflection.Register(server)

	// Create a channel to control the buying attempts
	buyBreadChan := make(chan bool)

	pgConn := connectToDB()
	if pgConn == nil {
		log.Panic("Could not connect to database")
	}

	// Check bread every 10 seconds in the background
	go func() {
		log.Println("Starting to check bread")
		for {
			err := checkBread(pgConn)
			if err != nil {
				log.Printf("Failed to check bread: %v", err)
			}
			time.Sleep(10 * time.Second)
		}
	}()

	// Start a goroutine to buy bread
	go func() {
		for {
			// Wait for a signal to buy bread
			<-buyBreadChan
			performBuyBread(pgConn)
		}
	}()

	go func() {
		// Regularly signal the goroutine to buy bread
		for true {
			log.Println("Iterating to buy bread...")
			buyBreadChan <- true
			time.Sleep(3 * time.Second)
		}
	}()

	// Start gRPC Server
	if err = server.Serve(listen); err != nil {
		log.Fatalf("Failed to serve gRPC server over %v", err)
	}

}
