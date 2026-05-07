package testutils

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	rabbitmq "github.com/rabbitmq/amqp091-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	log "github.com/sirupsen/logrus"
)

// TestHarness manages the full test infrastructure for integration and E2E tests.
//
// It provides:
//   - Access to RabbitMQ connection for publishing test messages
//   - Database connection for assertions
//   - gRPC client connection to the server
//   - Queue purge and DB cleanup helpers
//   - Service helper clients (BrokerService, BuyBread, etc.)
//
// Usage:
//
//	harness := testutils.NewTestHarness(t)
//	defer harness.Cleanup()
//
//	// Test setup
//	harness.PurgeQueues()
//	harness.ClearDB()
//
//	// Publish to RabbitMQ
//	harness.PublishBuyOrder(orderJSON)
//
//	// Assert via gRPC
//	client := pb.NewBrokerServiceClient(harness.GRPCConn())
//
//	// Assert via DB
//	harness.DB()
type TestHarness struct {
	t        *testing.T
	db       *sql.DB
	dbDSN    string
	rmqConn  *rabbitmq.Connection
	rmqCh    *rabbitmq.Channel
	rmqURL   string
	grpcAddr string
}

// NewTestHarness creates a new test harness.
// It starts Postgres and RabbitMQ containers if they are not already running.
func NewTestHarness(t *testing.T) *TestHarness {
	log.SetLevel(log.ErrorLevel)

	h := &TestHarness{
		t:        t,
		dbDSN:    "postgres://postgres:password@localhost:5432/bakery?sslmode=disable",
		rmqURL:   "amqp://guest:guest@localhost:5672/",
		grpcAddr: "localhost:50051",
	}

	// Ensure infrastructure is running
	if !isContainerRunning("bakery-postgres") || !isContainerRunning("bakery-rabbitmq") {
		projectDir := findProjectDir(t)
		if err := startInfraContainers(projectDir); err != nil {
			t.Fatalf("Failed to start infrastructure containers: %v", err)
		}
	}

	// Wait for Postgres
	var err error
	h.db, err = waitForDB(h.dbDSN, 60*time.Second)
	if err != nil {
		t.Fatalf("Postgres not ready: %v", err)
	}

	// Wait for RabbitMQ
	if err := waitForRabbitMQ(h.rmqURL, 30*time.Second); err != nil {
		t.Fatalf("RabbitMQ not ready: %v", err)
	}

	// Connect to RabbitMQ for test message publishing
	rmqConn, err := rabbitmq.Dial(h.rmqURL)
	if err != nil {
		t.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	h.rmqConn = rmqConn

	rmqCh, err := rmqConn.Channel()
	if err != nil {
		t.Fatalf("Failed to open RabbitMQ channel: %v", err)
	}
	h.rmqCh = rmqCh

	return h
}

// WaitForServer waits for the gRPC server to be ready.
func (h *TestHarness) WaitForServer(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := grpc.Dial(h.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("gRPC server not ready at %s after %s", h.grpcAddr, timeout)
}

// GRPCConn returns a new gRPC client connection to the test server.
func (h *TestHarness) GRPCConn() *grpc.ClientConn {
	conn, err := grpc.Dial(h.grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		h.t.Fatalf("Failed to dial gRPC: %v", err)
	}
	return conn
}

// BrokerServiceClient returns a gRPC client for the BrokerService.
func (h *TestHarness) BrokerServiceClient() pb.BrokerServiceClient {
	conn := h.GRPCConn()
	return pb.NewBrokerServiceClient(conn)
}

// BuyBreadClient returns a gRPC client for the BuyBread service.
func (h *TestHarness) BuyBreadClient() pb.BuyBreadClient {
	conn := h.GRPCConn()
	return pb.NewBuyBreadClient(conn)
}

// AdminServiceClient returns a gRPC client for the AdminService.
func (h *TestHarness) AdminServiceClient() pb.AdminServiceClient {
	conn := h.GRPCConn()
	return pb.NewAdminServiceClient(conn)
}

// RabbitMQConn returns the shared RabbitMQ connection for test message publishing.
func (h *TestHarness) RabbitMQConn() *rabbitmq.Connection {
	return h.rmqConn
}

// RabbitMQChannel returns the shared RabbitMQ channel for test operations.
func (h *TestHarness) RabbitMQChannel() *rabbitmq.Channel {
	return h.rmqCh
}

// DB returns the database connection.
func (h *TestHarness) DB() *sql.DB {
	return h.db
}

// GetGRPCAddress returns the gRPC address the test server is listening on.
func (h *TestHarness) GetGRPCAddress() string {
	return h.grpcAddr
}

// GetDBDSN returns the database DSN.
func (h *TestHarness) GetDBDSN() string {
	return h.dbDSN
}

// Cleanup stops Docker containers if we started them.
// Note: the DB and RabbitMQ connections are left open — the test framework
// keeps the infrastructure running for other test packages.
func (h *TestHarness) Cleanup() {
	// Connections are shared; don't close them.
	// The fixture lifecycle manages infrastructure.
}

// PurgeQueues purges all RabbitMQ queues used by the bakery system.
func (h *TestHarness) PurgeQueues() error {
	queues := []string{"buy-bread-order", "bread-bought", "make-bread-order", "bread-made"}
	for _, q := range queues {
		if _, err := h.rmqCh.QueuePurge(q, false); err != nil {
			// Queue might not exist yet — ignore
			log.Debugf("[test] Queue purge warning for %s: %v", q, err)
		}
	}
	return nil
}

// DeclareQueues ensures all bakery queues exist (idempotent).
func (h *TestHarness) DeclareQueues() error {
	queues := []string{"buy-bread-order", "bread-bought", "make-bread-order", "bread-made"}
	for _, q := range queues {
		if _, err := h.rmqCh.QueueDeclare(q, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare queue %s: %w", q, err)
		}
	}
	return nil
}

// ClearDB clears all tables and resets sequences.
func (h *TestHarness) ClearDB() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tables := []string{
		"invoice_items", "invoices", "admin_users", "orders_processed",
		"order_details", "buy_order", "customer", "make_order_details",
		"make_order", "bread_maker", "pending_make_orders", "bread", "outbox",
	}
	for _, table := range tables {
		h.db.ExecContext(ctx, "DELETE FROM "+table)
	}

	sequences := []string{
		"customer_id_seq", "buy_id_seq", "bread_id_seq", "bread_maker_id_seq",
		"make_order_id_seq", "pending_make_order_id_seq", "orders_processed_id_seq",
		"invoice_id_seq", "admin_user_id_seq", "invoice_item_id_seq",
	}
	for _, seq := range sequences {
		h.db.ExecContext(ctx, "ALTER SEQUENCE "+seq+" RESTART WITH 1")
	}
	return nil
}

// PublishToQueue publishes a message to a RabbitMQ queue.
func (h *TestHarness) PublishToQueue(queueName string, body []byte) error {
	return h.rmqCh.Publish(
		"", queueName, false, false,
		rabbitmq.Publishing{
			ContentType: "application/json",
			Body:        body,
		},
	)
}

// ConsumeFromQueue consumes a single message from a queue.
func (h *TestHarness) ConsumeFromQueue(queueName string, timeout time.Duration) ([]byte, error) {
	consumer, err := h.rmqCh.Consume(
		queueName, "", false, false, false, false, nil,
	)
	if err != nil {
		return nil, err
	}

	select {
	case d := <-consumer:
		d.Ack(false)
		return d.Body, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for message on %s", queueName)
	}
}

// PublishBuyOrder publishes a serialized BuyOrder to the buy-bread-order queue.
func (h *TestHarness) PublishBuyOrder(orderJSON []byte) error {
	return h.PublishToQueue("buy-bread-order", orderJSON)
}

// PublishMakeBreadOrder publishes a serialized make-bread message to the make-bread-order queue.
func (h *TestHarness) PublishMakeBreadOrder(msgJSON []byte) error {
	return h.PublishToQueue("make-bread-order", msgJSON)
}

// ConsumeBuyOrder consumes a single buy order from the buy-bread-order queue.
func (h *TestHarness) ConsumeBuyOrder(timeout time.Duration) (*data.BuyOrder, error) {
	body, err := h.ConsumeFromQueue("buy-bread-order", timeout)
	if err != nil {
		return nil, err
	}
	var order data.BuyOrder
	if err := json.Unmarshal(body, &order); err != nil {
		return nil, fmt.Errorf("unmarshal buy order: %w", err)
	}
	return &order, nil
}

// ConsumeBreadBought consumes a single bread-bought message from the bread-bought queue.
func (h *TestHarness) ConsumeBreadBought(timeout time.Duration) (*data.BuyOrder, error) {
	body, err := h.ConsumeFromQueue("bread-bought", timeout)
	if err != nil {
		return nil, err
	}
	var order data.BuyOrder
	if err := json.Unmarshal(body, &order); err != nil {
		// Try the nested format (publishResult)
		var pr struct {
			Order data.BuyOrder `json:"order"`
		}
		if err2 := json.Unmarshal(body, &pr); err2 != nil {
			return nil, fmt.Errorf("unmarshal bread-bought: %w", err)
		}
		return &pr.Order, nil
	}
	return &order, nil
}

// breadMadeConfirmation represents a bread-made confirmation message.
type breadMadeConfirmation struct {
	BreadID  int `json:"breadId"`
	Quantity int `json:"quantity"`
}

// ConsumeBreadMade consumes a single bread-made confirmation.
func (h *TestHarness) ConsumeBreadMade(timeout time.Duration) (*breadMadeConfirmation, error) {
	body, err := h.ConsumeFromQueue("bread-made", timeout)
	if err != nil {
		return nil, err
	}
	var confirmation breadMadeConfirmation
	if err := json.Unmarshal(body, &confirmation); err != nil {
		return nil, fmt.Errorf("unmarshal bread-made: %w", err)
	}
	return &confirmation, nil
}

// findProjectDir walks up the filesystem to find the project root.
func findProjectDir(t *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(wd + "/docker-compose.yml"); err == nil {
			return wd
		}
		dir := wd[:len(wd)-1]
		for len(dir) > 0 && dir[len(dir)-1] != '/' {
			dir = dir[:len(dir)-1]
		}
		wd = dir
	}
	t.Fatal("Could not find project root")
	return ""
}

// startInfraContainers starts Postgres and RabbitMQ via docker run.
func startInfraContainers(projectDir string) error {
	sqlFile := projectDir + "/bakery.sql"
	seedFile := projectDir + "/seed-dev.sql"

	// Create network
	exec.Command("docker", "network", "create", "bakery-network").Run() //nolint:errcheck

	// PostgreSQL
	exec.Command("docker", "rm", "-f", "bakery-postgres").Run() //nolint:errcheck
	args := []string{
		"run", "-d",
		"--name", "bakery-postgres",
		"--network", "bakery-network",
		"-e", "POSTGRES_USER=postgres",
		"-e", "POSTGRES_PASSWORD=password",
		"-e", "POSTGRES_DB=bakery",
		"-v", sqlFile + ":/docker-entrypoint-initdb.d/01-schema.sql:ro",
		"-v", seedFile + ":/docker-entrypoint-initdb.d/02-seed-dev.sql:ro",
		"-p", "5432:5432",
		"postgres:18-alpine",
	}
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("start postgres: %v\n%s", err, out)
	}

	// RabbitMQ
	exec.Command("docker", "rm", "-f", "bakery-rabbitmq").Run() //nolint:errcheck
	args = []string{
		"run", "-d",
		"--name", "bakery-rabbitmq",
		"--network", "bakery-network",
		"-e", "RABBITMQ_DEFAULT_USER=guest",
		"-e", "RABBITMQ_DEFAULT_PASS=guest",
		"-p", "5672:5672",
		"-p", "15672:15672",
		"rabbitmq:3-management-alpine",
	}
	if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("start rabbitmq: %v\n%s", err, out)
	}

	return nil
}

// waitForRabbitMQ waits for RabbitMQ to accept AMQP connections.
func waitForRabbitMQ(rmqURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := rabbitmq.Dial(rmqURL)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("RabbitMQ not ready after %s", timeout)
}

// SetupDB sets up the database schema and seed data.
func (h *TestHarness) SetupDB() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	projectDir := findProjectDir(h.t)

	// Run schema
	sqlFile := projectDir + "/bakery.sql"
	data, err := os.ReadFile(sqlFile)
	if err != nil {
		return fmt.Errorf("read schema file: %w", err)
	}
	if _, err := h.db.ExecContext(ctx, string(data)); err != nil {
		return fmt.Errorf("execute schema: %w", err)
	}

	// Run seed data
	seedFile := projectDir + "/seed-dev.sql"
	data, err = os.ReadFile(seedFile)
	if err != nil {
		return fmt.Errorf("read seed file: %w", err)
	}
	if _, err := h.db.ExecContext(ctx, string(data)); err != nil {
		return fmt.Errorf("execute seed: %w", err)
	}

	return nil
}

// OrderItemCount returns the number of items in a buy order.
func (h *TestHarness) OrderItemCount(orderUUID string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var count int
	err := h.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM order_details WHERE buy_order_id = (SELECT id FROM buy_order WHERE buy_order_uuid = $1)",
		orderUUID,
	).Scan(&count)
	return count, err
}

// BuyOrderCount returns the number of buy orders with the given UUID.
func (h *TestHarness) BuyOrderCount(orderUUID string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var count int
	err := h.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM buy_order WHERE buy_order_uuid = $1", orderUUID,
	).Scan(&count)
	return count, err
}

// BuyOrderStatus returns the status of a buy order.
func (h *TestHarness) BuyOrderStatus(orderUUID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var status string
	err := h.db.QueryRowContext(ctx,
		"SELECT status FROM buy_order WHERE buy_order_uuid = $1", orderUUID,
	).Scan(&status)
	return status, err
}

// BreadQuantity returns the current quantity of a bread item.
func (h *TestHarness) BreadQuantity(breadID int) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var qty int
	err := h.db.QueryRowContext(ctx,
		"SELECT quantity FROM bread WHERE id = $1", breadID,
	).Scan(&qty)
	return qty, err
}

// PendingMakeOrderCount returns the number of pending make orders for a bread.
func (h *TestHarness) PendingMakeOrderCount(breadID int) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var count int
	err := h.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM pending_make_orders WHERE bread_id = $1 AND status = 'pending'",
		breadID,
	).Scan(&count)
	return count, err
}
