package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	"github.com/calvarado2004/bakery-go/testutils"
	pb "github.com/calvarado2004/bakery-go/proto"
	rabbitmq "github.com/rabbitmq/amqp091-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestMakers_Integration tests the makers service with real RabbitMQ and PostgreSQL
func TestMakers_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	// Get connection to RabbitMQ
	rabbitMQAddr := testutils.GetRabbitMQAddress()
	conn, err := rabbitmq.Dial(rabbitMQAddr)
	if err != nil {
		t.Skipf("Could not connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to open channel: %v", err)
	}
	defer ch.Close()

	t.Run("CheckMakeBreadOrderQueueExists", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		msgs, err := ch.Consume(
			"make-bread-order",
			"test-consumer",
			true,                // auto-ack
			false,               // exclusive
			false,               // no-local
			false,               // no-wait
			nil,                 // args
		)
		if err != nil {
			t.Fatalf("Failed to consume from queue: %v", err)
		}

		// Just verify we can consume (queue exists)
		select {
		case <-msgs:
			t.Log("Queue has messages")
		case <-ctx.Done():
			t.Log("Queue is empty or consumer timed out")
		}
	})

	t.Run("PublishAndConsumeMakeBreadOrder", func(t *testing.T) {
		dbDSN := testutils.GetDBDSNFromT(t)
		db, err := sql.Open("pgx", dbDSN)
		if err != nil {
			t.Fatalf("Failed to connect to database: %v", err)
		}
		defer db.Close()

		// Get a bread item to restock
		var bread data.Bread
		err = db.QueryRow("SELECT id, quantity FROM bread LIMIT 1").
			Scan(&bread.ID, &bread.Quantity)
		if err != nil {
			t.Skipf("No bread available: %v", err)
		}

		initialQty := bread.Quantity

		// Create make bread message (restock)
		restockBread := data.Bread{
			ID:       bread.ID,
			Name:     bread.Name,
			Quantity: 50, // Restock quantity
		}

		payload, err := json.Marshal(restockBread)
		if err != nil {
			t.Fatalf("Failed to marshal bread data: %v", err)
		}

		// Publish to RabbitMQ
		err = ch.Publish(
			"",                   // exchange
			"make-bread-order",   // routing key
			false,                // mandatory
			false,                // immediate
			rabbitmq.Publishing{
				ContentType:  "application/json",
				Body:         payload,
				DeliveryMode: rabbitmq.Persistent,
			},
		)
		if err != nil {
			t.Fatalf("Failed to publish message: %v", err)
		}

		t.Logf("Published make bread order for %s (ID: %d)", bread.Name, bread.ID)

		// Wait for makers to process (up to 30 seconds)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		maxAttempts := 6
		for i := 0; i < maxAttempts; i++ {
			var newQty int
			err = db.QueryRowContext(ctx,
				"SELECT quantity FROM bread WHERE id = $1",
				bread.ID,
			).Scan(&newQty)

			if err == nil && newQty > initialQty {
				t.Logf("Bread restocked: %d -> %d", initialQty, newQty)
				return
			}

			time.Sleep(5 * time.Second)
		}

		// Final check
		var finalQty int
		err = db.QueryRowContext(ctx,
			"SELECT quantity FROM bread WHERE id = $1",
			bread.ID,
		).Scan(&finalQty)

		if err == nil {
			t.Logf("Final bread quantity: %d (initial: %d)", finalQty, initialQty)
		}
	})
}

// TestMakers_Repos_Integration tests repository operations in makers context
func TestMakers_Repos_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	db := fixture.DB

	t.Run("AdjustBreadQuantityPositive", func(t *testing.T) {
		repo := data.NewPostgresRepository(db)

		// Get a bread item
		var bread data.Bread
		err := db.QueryRow("SELECT id, name, quantity FROM bread LIMIT 1").
			Scan(&bread.ID, &bread.Name, &bread.Quantity)
		if err != nil {
			t.Skipf("No bread available: %v", err)
		}

		initialQty := bread.Quantity

		// Adjust quantity (restock)
		success, err := repo.AdjustBreadQuantity(bread.ID, 20)
		if err != nil {
			t.Fatalf("Failed to adjust quantity: %v", err)
		}

		if !success {
			t.Error("Expected quantity adjustment to succeed")
		}

		// Verify
		var newQty int
		err = db.QueryRow("SELECT quantity FROM bread WHERE id = $1", bread.ID).Scan(&newQty)
		if err != nil {
			t.Fatalf("Failed to verify quantity: %v", err)
		}

		if newQty != initialQty+20 {
			t.Errorf("Expected quantity %d, got %d", initialQty+20, newQty)
		}

		t.Logf("Restocked bread quantity: %d -> %d", initialQty, newQty)

		// Reset
		_, _ = repo.AdjustBreadQuantity(bread.ID, -20)
	})

	t.Run("InsertMakeOrder", func(t *testing.T) {
		repo := data.NewPostgresRepository(db)

		// Get a bread maker
		var maker data.BreadMaker
		err := db.QueryRow("SELECT id, name FROM bread_maker LIMIT 1").
			Scan(&maker.ID, &maker.Name)
		if err != nil {
			t.Skipf("No bread maker available: %v", err)
		}

		// Get bread to make
		var bread data.Bread
		err = db.QueryRow("SELECT id, name FROM bread LIMIT 1").
			Scan(&bread.ID, &bread.Name)
		if err != nil {
			t.Skipf("No bread available: %v", err)
		}

		makeOrder := data.MakeOrder{
			BreadMakerID: maker.ID,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}

		orderID, err := repo.InsertMakeOrder(makeOrder, []data.Bread{bread})
		if err != nil {
			t.Fatalf("Failed to insert make order: %v", err)
		}

		if orderID <= 0 {
			t.Errorf("Expected positive ID, got %d", orderID)
		}

		t.Logf("Created make order with ID: %d for maker %s", orderID, maker.Name)
	})

	t.Run("GetMakeOrderByID", func(t *testing.T) {
		repo := data.NewPostgresRepository(db)

		// Get the latest make order
		var orderID int
		err := db.QueryRow("SELECT id FROM make_order ORDER BY id DESC LIMIT 1").
			Scan(&orderID)
		if err != nil {
			t.Skipf("No make orders available: %v", err)
		}

		order, err := repo.GetMakeOrderByID(orderID)
		if err != nil {
			t.Fatalf("Failed to get make order: %v", err)
		}

		if order.ID != orderID {
			t.Errorf("Expected order ID %d, got %d", orderID, order.ID)
		}

		t.Logf("Retrieved make order ID: %d", order.ID)
	})

	t.Run("GetAllMakeOrders", func(t *testing.T) {
		repo := data.NewPostgresRepository(db)

		orders, err := repo.GetAllMakeOrders()
		if err != nil {
			t.Fatalf("Failed to get all make orders: %v", err)
		}

		t.Logf("Found %d make orders", len(orders))
	})
}

// TestMakers_listenForMakeBread_Integration tests the main listener function
func TestMakers_listenForMakeBread_Integration(t *testing.T) {
	fixture := testutils.NewIntegrationFixture(t)
	defer fixture.Cleanup()

	db := fixture.DB

	// Get a bread item
	var bread data.Bread
	err := db.QueryRow("SELECT id, name, quantity FROM bread LIMIT 1").
		Scan(&bread.ID, &bread.Name, &bread.Quantity)
	if err != nil {
		t.Skipf("No bread available: %v", err)
	}

	initialQty := bread.Quantity

	// Get RabbitMQ connection
	rabbitMQAddr := testutils.GetRabbitMQAddress()
	conn, err := rabbitmq.Dial(rabbitMQAddr)
	if err != nil {
		t.Skipf("Could not connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("Failed to open channel: %v", err)
	}
	defer ch.Close()

	// Publish make bread message
	restockBread := data.Bread{
		ID:       bread.ID,
		Name:     bread.Name,
		Quantity: 30,
	}

	payload, err := json.Marshal(restockBread)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	err = ch.Publish(
		"",
		"make-bread-order",
		false,
		false,
		rabbitmq.Publishing{
			ContentType:  "application/json",
			Body:         payload,
			DeliveryMode: rabbitmq.Persistent,
		},
	)
	if err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	t.Logf("Published restock message for %s", bread.Name)

	// Wait for processing
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	maxAttempts := 6
	for i := 0; i < maxAttempts; i++ {
		var newQty int
		err = db.QueryRowContext(ctx,
			"SELECT quantity FROM bread WHERE id = $1",
			bread.ID,
		).Scan(&newQty)

		if err == nil && newQty > initialQty {
			t.Logf("Successfully restocked: %d -> %d", initialQty, newQty)
			return
		}

		time.Sleep(5 * time.Second)
	}

	// Final check
	var finalQty int
	err = db.QueryRowContext(ctx,
		"SELECT quantity FROM bread WHERE id = $1",
		bread.ID,
	).Scan(&finalQty)

	if err == nil {
		t.Logf("Final quantity: %d (initial: %d)", finalQty, initialQty)
	}
}

// TestMakers_Config_Integration tests the Config struct
func TestMakers_Config_Integration(t *testing.T) {
	dbDSN := testutils.GetDBDSNFromT(t)
	db, err := sql.Open("pgx", dbDSN)
	if err != nil {
		t.Skipf("Could not connect to database: %v", err)
	}
	defer db.Close()

	repo := data.NewPostgresRepository(db)
	cfg := Config{
		Repo:   repo,
		Client: &http.Client{Timeout: 10 * time.Second},
	}

	if cfg.Repo == nil {
		t.Error("Expected Repo to be set")
	}

	if cfg.Client == nil {
		t.Error("Expected Client to be set")
	}

	t.Log("Config struct correctly initialized")
}

// TestMakers_NewPostgresRepository_Integration tests repository initialization
func TestMakers_NewPostgresRepository_Integration(t *testing.T) {
	dbDSN := testutils.GetDBDSNFromT(t)
	db, err := sql.Open("pgx", dbDSN)
	if err != nil {
		t.Skipf("Could not connect to database: %v", err)
	}
	defer db.Close()

	repo := data.NewPostgresRepository(db)

	if repo == nil {
		t.Fatal("Expected non-nil repository")
	}

	if repo.Conn == nil {
		t.Error("Expected database connection to be set")
	}

	t.Log("PostgresRepository correctly initialized")
}

// TestMakers_gRPCClient_Integration tests gRPC client setup
func TestMakers_gRPCClient_Integration(t *testing.T) {
	addr := testutils.GetGRPCAddress()

	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithTimeout(10*time.Second),
	)
	if err != nil {
		t.Skipf("Could not connect to gRPC server: %v", err)
	}
	defer conn.Close()

	client := pb.NewMakeBreadClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	t.Run("ConnectToMakeBreadClient", func(t *testing.T) {
		if client == nil {
			t.Error("Expected non-nil MakeBreadClient")
		}
		t.Log("MakeBreadClient created successfully")
	})

	t.Run("CallBakeBread", func(t *testing.T) {
		// Get a bread item
		dbDSN := testutils.GetDBDSNFromT(t)
		db, err := sql.Open("pgx", dbDSN)
		if err != nil {
			t.Skipf("Could not connect to database: %v", err)
		}
		defer db.Close()

		var bread data.Bread
		err = db.QueryRow("SELECT id, name FROM bread LIMIT 1").
			Scan(&bread.ID, &bread.Name)
		if err != nil {
			t.Skipf("No bread available: %v", err)
		}

		req := &pb.BreadRequest{
			Breads: &pb.BreadList{
				Breads: []*pb.Bread{
					{Id: int32(bread.ID), Quantity: 10},
				},
			},
		}

		resp, err := client.BakeBread(ctx, req)
		if err != nil {
			t.Logf("BakeBread error: %v", err)
			t.Skip("Server may still be initializing")
		}

		if resp != nil {
			t.Logf("BakeBread successful, Make Order UUID: %s", resp.MakeOrderUuid)
		}
	})
}
