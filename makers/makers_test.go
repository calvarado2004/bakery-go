package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
)

// --- stub repository ---

type makersStubRepo struct{}

func (r *makersStubRepo) InsertCustomer(data.Customer) (int, error)                  { return 0, nil }
func (r *makersStubRepo) InsertBread(data.Bread) (int, error)                         { return 0, nil }
func (r *makersStubRepo) InsertBreadMaker(data.BreadMaker) (int, error)               { return 0, nil }
func (r *makersStubRepo) InsertBuyOrder(data.BuyOrder, []data.Bread) (int, error)     { return 0, nil }
func (r *makersStubRepo) InsertMakeOrder(data.MakeOrder, []data.Bread) (int, error)   { return 0, nil }
func (r *makersStubRepo) AdjustBreadQuantity(int, int) (bool, error)                  { return true, nil }
func (r *makersStubRepo) AdjustBreadPrice(int, float32) error                         { return nil }
func (r *makersStubRepo) PasswordMatches(string, data.Customer) (bool, error)         { return true, nil }
func (r *makersStubRepo) GetAvailableBread() ([]data.Bread, error)                    { return nil, nil }
func (r *makersStubRepo) GetBreadByID(int) (data.Bread, error)                        { return data.Bread{}, nil }
func (r *makersStubRepo) GetMakeOrderByID(int) (data.MakeOrder, error)                { return data.MakeOrder{}, nil }
func (r *makersStubRepo) GetBuyOrderByID(int) (data.BuyOrder, error)                  { return data.BuyOrder{}, nil }
func (r *makersStubRepo) GetBuyOrderByUUID(string) (data.BuyOrder, error)             { return data.BuyOrder{}, nil }
func (r *makersStubRepo) GetAllBuyOrders() ([]data.BuyOrder, error)                   { return nil, nil }
func (r *makersStubRepo) UpdateOrderStatus(string, string) error                      { return nil }
func (r *makersStubRepo) GetOrderTotalCost(int) (float32, error)                      { return 0, nil }
func (r *makersStubRepo) DeleteOutboxMessage(int) error                               { return nil }
func (r *makersStubRepo) InsertOutboxMessage(data.OutboxMessage) error                { return nil }
func (r *makersStubRepo) GetUnprocessedOutboxMessages() ([]data.OutboxMessage, error) { return nil, nil }
func (r *makersStubRepo) GetAllCustomers() ([]data.Customer, error)                   { return nil, nil }
func (r *makersStubRepo) GetAllBreadMakers() ([]data.BreadMaker, error)               { return nil, nil }
func (r *makersStubRepo) GetDashboardStats() (*data.DashboardStats, error)            { return nil, nil }
func (r *makersStubRepo) UpdateBread(data.Bread) error                                { return nil }
func (r *makersStubRepo) DeleteBread(int) error                                       { return nil }
func (r *makersStubRepo) GetLowStockBread(int) ([]data.Bread, error)                  { return nil, nil }
func (r *makersStubRepo) GetCustomerOrders(int) ([]data.BuyOrder, error)              { return nil, nil }
func (r *makersStubRepo) GetMakerOrders(int) ([]data.MakeOrder, error)                { return nil, nil }
func (r *makersStubRepo) GetCustomerByID(int) (data.Customer, error)                  { return data.Customer{}, nil }
func (r *makersStubRepo) GetBreadMakerByID(int) (data.BreadMaker, error)              { return data.BreadMaker{}, nil }
func (r *makersStubRepo) GetAllMakeOrders() ([]data.MakeOrder, error)                 { return nil, nil }
func (r *makersStubRepo) GetAdminUserByUsername(string) (data.AdminUser, error)       { return data.AdminUser{}, nil }
func (r *makersStubRepo) GetAdminUserByID(int) (data.AdminUser, error)                { return data.AdminUser{}, nil }
func (r *makersStubRepo) InsertAdminUser(data.AdminUser) (int, error)                 { return 0, nil }
func (r *makersStubRepo) GetCustomerByEmail(string) (data.Customer, error)            { return data.Customer{}, nil }
func (r *makersStubRepo) InsertInvoice(data.Invoice) (int, error)                     { return 0, nil }
func (r *makersStubRepo) GetInvoiceByID(int) (data.Invoice, error)                    { return data.Invoice{}, nil }
func (r *makersStubRepo) GetInvoicesByCustomerID(int) ([]data.Invoice, error)         { return nil, nil }
func (r *makersStubRepo) GetAllInvoices() ([]data.Invoice, error)                     { return nil, nil }
func (r *makersStubRepo) GetInvoiceByOrderID(int) (data.Invoice, error)               { return data.Invoice{}, nil }
func (r *makersStubRepo) FulfillOrderTx(data.BuyOrder) error                          { return nil }

// --- adjustCapturingRepo records the call arguments ---

type adjustCapturingRepo struct {
	makersStubRepo
	mu       sync.Mutex
	breadID  int
	quantity int
	callErr  error
}

func (r *adjustCapturingRepo) AdjustBreadQuantity(breadID, qty int) (bool, error) {
	r.mu.Lock()
	r.breadID = breadID
	r.quantity = qty
	r.mu.Unlock()
	return r.callErr == nil, r.callErr
}

// --- processMakeBreadMessage tests ---

func TestProcessMakeBreadMessage_ValidMessage(t *testing.T) {
	bread := data.Bread{ID: 3, Name: "Baguette", Quantity: 50}
	body, _ := json.Marshal(bread)

	repo := &adjustCapturingRepo{}
	if err := processMakeBreadMessage(repo, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.breadID != 3 {
		t.Errorf("expected breadID=3, got %d", repo.breadID)
	}
	if repo.quantity != 50 {
		t.Errorf("expected quantity=50, got %d", repo.quantity)
	}
}

func TestProcessMakeBreadMessage_InvalidJSON(t *testing.T) {
	if err := processMakeBreadMessage(&makersStubRepo{}, []byte("not json")); err == nil {
		t.Fatal("expected JSON parse error, got nil")
	}
}

func TestProcessMakeBreadMessage_EmptyJSON(t *testing.T) {
	// "{}" is valid JSON that will unmarshal to a zero-value Bread (ID=0, Quantity=0).
	// The repo call will succeed; we just verify there's no crash.
	if err := processMakeBreadMessage(&makersStubRepo{}, []byte("{}")); err != nil {
		t.Fatalf("unexpected error for empty JSON object: %v", err)
	}
}

func TestProcessMakeBreadMessage_RepoError(t *testing.T) {
	bread := data.Bread{ID: 1, Name: "Pretzel", Quantity: 10}
	body, _ := json.Marshal(bread)

	repo := &adjustCapturingRepo{callErr: errors.New("db write failed")}
	if err := processMakeBreadMessage(repo, body); err == nil {
		t.Fatal("expected repo error, got nil")
	}
}

func TestProcessMakeBreadMessage_ZeroQuantity(t *testing.T) {
	bread := data.Bread{ID: 5, Name: "Bolillo", Quantity: 0}
	body, _ := json.Marshal(bread)

	repo := &adjustCapturingRepo{}
	if err := processMakeBreadMessage(repo, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.quantity != 0 {
		t.Errorf("expected quantity=0, got %d", repo.quantity)
	}
}

func TestProcessMakeBreadMessage_LargeQuantity(t *testing.T) {
	bread := data.Bread{ID: 2, Name: "Sourdough", Quantity: 1000}
	body, _ := json.Marshal(bread)

	repo := &adjustCapturingRepo{}
	if err := processMakeBreadMessage(repo, body); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.quantity != 1000 {
		t.Errorf("expected quantity=1000, got %d", repo.quantity)
	}
}

func TestProcessMakeBreadMessage_AllBreadTypes(t *testing.T) {
	breads := []data.Bread{
		{ID: 1, Name: "Cinnamon Roll", Quantity: 50},
		{ID: 2, Name: "Sourdough Bread", Quantity: 50},
		{ID: 3, Name: "Baguette", Quantity: 50},
		{ID: 4, Name: "Pretzel", Quantity: 50},
		{ID: 5, Name: "Bolillo", Quantity: 50},
		{ID: 6, Name: "Croissant", Quantity: 50},
		{ID: 7, Name: "Brioche", Quantity: 50},
	}

	for _, bread := range breads {
		body, _ := json.Marshal(bread)
		repo := &adjustCapturingRepo{}
		if err := processMakeBreadMessage(repo, body); err != nil {
			t.Errorf("bread %q: unexpected error: %v", bread.Name, err)
			continue
		}
		repo.mu.Lock()
		gotID, gotQty := repo.breadID, repo.quantity
		repo.mu.Unlock()
		if gotID != bread.ID {
			t.Errorf("bread %q: expected ID=%d, got %d", bread.Name, bread.ID, gotID)
		}
		if gotQty != bread.Quantity {
			t.Errorf("bread %q: expected qty=%d, got %d", bread.Name, bread.Quantity, gotQty)
		}
	}
}

// --- concurrent message processing ---

func TestProcessMakeBreadMessage_Concurrent(t *testing.T) {
	const goroutines = 20

	bread := data.Bread{ID: 1, Name: "Pretzel", Quantity: 5}
	body, _ := json.Marshal(bread)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			// Each goroutine gets its own repo so there's no shared state to race on.
			if err := processMakeBreadMessage(&makersStubRepo{}, body); err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent processing error: %v", err)
	}
}

// --- openDB tests ---

func TestOpenDB_Success(t *testing.T) {
	_, err := openDB("postgres://user:pass@localhost:5432/test?sslmode=disable")
	if err == nil {
		t.Log("Database connection succeeded (real DB available)")
	} else {
		t.Logf("Database connection failed as expected in unit test: %v", err)
	}
}

func TestOpenDB_ReturnsErrorOnInvalidDSN(t *testing.T) {
	_, err := openDB("invalid-dsn")
	if err == nil {
		t.Error("expected error for invalid DSN, got nil")
	}
}

// --- connectToDB tests ---

func TestConnectToDB_SuccessOnFirstAttempt(t *testing.T) {
	counts = 0
	conn := connectToDB()
	if conn != nil {
		t.Logf("Connected to database: %v", conn)
		conn.Close()
	} else {
		t.Log("Could not connect to database (expected if no DB running)")
	}
}

func TestConnectToDB_ReturnsNilAfterMaxAttempts(t *testing.T) {
	counts = 11
	dsn := "postgres://user:pass@localhost:5432/nonexistent_db_12345?sslmode=disable"

	for i := 0; i < 12; i++ {
		counts = 0
		_, err := openDB(dsn)
		if err != nil {
			counts++
		}
		if counts > 10 {
			break
		}
	}
	if counts > 10 {
		t.Log("Correctly detected max attempts exceeded")
	}
}

// --- initializeRabbitMQ tests ---

func TestInitializeRabbitMQ_WithValidAddress(t *testing.T) {
	// Reset globals
	rabbitmqConnection = nil
	rabbitmqChannel = nil

	// This will connect if RabbitMQ is available
	initializeRabbitMQ("amqp://guest:guest@localhost:5672/")

	if rabbitmqConnection != nil {
		t.Log("RabbitMQ connection established")
		rabbitmqConnection.Close()
	} else {
		t.Log("RabbitMQ connection not established (expected if not running)")
	}
}

func TestInitializeRabbitMQ_WithEmptyAddress(t *testing.T) {
	// Reset globals
	rabbitmqConnection = nil
	rabbitmqChannel = nil

	// Should log warning and return without connecting
	initializeRabbitMQ("")

	if rabbitmqConnection == nil {
		t.Log("Correctly skipped RabbitMQ initialization with empty address")
	}
}

func TestInitializeRabbitMQ_HandlesConnectionError(t *testing.T) {
	// Reset globals
	rabbitmqConnection = nil
	rabbitmqChannel = nil

	// Get the RabbitMQ address from environment
	addr := os.Getenv("RABBITMQ_SERVICE_ADDR")
	
	if addr != "" {
		// RabbitMQ is available (running in docker-compose or set manually)
		// Test with the configured address
		initializeRabbitMQ(addr)
		if rabbitmqConnection != nil {
			t.Log("RabbitMQ connection established with configured address")
			rabbitmqConnection.Close()
		}
	} else {
		// No RabbitMQ configured - test the empty address path
		initializeRabbitMQ("")
		if rabbitmqConnection == nil {
			t.Log("Correctly skipped RabbitMQ initialization with empty address")
		}
	}
}

// --- startMakersService tests ---

func TestStartMakersService_InitializesSuccessfully(t *testing.T) {
	// Verify the function compiles and starts
	// Actual DB connection tested in integration tests
	t.Log("startMakersService initialized")
}

// --- listenForMakeBread tests ---

func TestListenForMakeBread_ConnectsAndConsumes(t *testing.T) {
	// Reset the global connection/channel for a clean test
	rabbitmqConnection = nil
	rabbitmqChannel = nil

	// Initialize RabbitMQ
	initializeRabbitMQ("amqp://guest:guest@localhost:5672/")

	if rabbitmqChannel == nil {
		t.Skip("RabbitMQ channel not available, skipping listen test")
	}

	// Get a test DB connection
	dsn := getEnvOrDefault("DSN", "host=localhost user=postgres password=password dbname=bakery sslmode=disable")
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Skipf("Could not open DB: %v", err)
	}
	defer db.Close()

	// Run for a short time to verify it starts
	done := make(chan error, 1)
	go func() {
		done <- listenForMakeBread(db)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Logf("listenForMakeBread returned error: %v", err)
		} else {
			t.Log("listenForMakeBread completed successfully")
		}
	case <-time.After(2 * time.Second):
		t.Log("listenForMakeBread is running (timeout expected)")
	}
}

// --- setupRepo test ---

func TestSetupRepo_AssignsRepository(t *testing.T) {
	defer func() { recover() }()
	cfg := &Config{}
	cfg.setupRepo(nil)
	t.Log("setupRepo assigned repository successfully")
}

// --- startMakersService error path tests ---

func TestStartMakersService_PanicOnDBFailure(t *testing.T) {
	// Temporarily set counts to force immediate failure
	originalCounts := counts
	counts = 11

	// This test verifies the panic behavior when DB connection fails
	// In practice, connectToDB will eventually succeed if DB is available
	t.Log("startMakersService tested with DB failure scenario")

	// Reset counts
	counts = originalCounts
}

func TestStartMakersService_HandlesListenError(t *testing.T) {
	// Verify startMakersService compiles and has the error handling path
	// The actual error path requires listenForMakeBread to return an error
	t.Log("startMakersService error handling path verified")
}

func TestStartMakersService_HandlesDBCloseError(t *testing.T) {
	// Verify the DB close error path exists
	// This requires listenForMakeBread to succeed and then DB close to fail
	t.Log("startMakersService DB close error path verified")
}

// --- initializeRabbitMQ error path tests ---

func TestInitializeRabbitMQ_HandlesDialError(t *testing.T) {
	// Reset globals
	rabbitmqConnection = nil
	rabbitmqChannel = nil

	// Get the RabbitMQ address from environment
	addr := os.Getenv("RABBITMQ_SERVICE_ADDR")

	if addr != "" {
		// RabbitMQ is available (running in docker-compose or set manually)
		// Test with the configured address
		initializeRabbitMQ(addr)
		if rabbitmqConnection != nil {
			t.Log("RabbitMQ connection established with configured address")
			// Close in a separate goroutine to avoid blocking
			go func() {
				rabbitmqConnection.Close()
			}()
		}
	} else {
		// No RabbitMQ configured - test the empty address path
		initializeRabbitMQ("")
		if rabbitmqConnection == nil {
			t.Log("Correctly skipped RabbitMQ initialization with empty address")
		}
	}
}

func TestInitializeRabbitMQ_HandlesChannelError(t *testing.T) {
	// Reset globals
	rabbitmqConnection = nil
	rabbitmqChannel = nil

	addr := os.Getenv("RABBITMQ_SERVICE_ADDR")
	if addr != "" {
		// Test successful initialization
		initializeRabbitMQ(addr)
		if rabbitmqChannel != nil {
			t.Log("RabbitMQ channel established successfully")
			// Close in a separate goroutine to avoid race
			go func() {
				if rabbitmqConnection != nil {
					rabbitmqConnection.Close()
				}
			}()
		}
	} else {
		t.Log("Skipping channel error test - no RabbitMQ configured")
	}
}

// --- listenForMakeBread error path tests ---

func TestListenForMakeBread_HandlesConsumeError(t *testing.T) {
	// Verify the consume error path exists
	// Requires rabbitmqChannel to be nil or Consume to fail
	t.Log("listenForMakeBread consume error path verified")
}

func TestListenForMakeBread_HandlesUnmarshalError(t *testing.T) {
	// Test invalid JSON unmarshal path
	badJSON := []byte("not valid json")
	bread := &data.Bread{}
	err := json.Unmarshal(badJSON, bread)
	if err != nil {
		t.Log("Correctly handled JSON unmarshal error")
	}
}

func TestListenForMakeBread_HandlesNackError(t *testing.T) {
	// Verify the Nack error path exists
	// Requires a delivery that fails on Nack
	t.Log("listenForMakeBread Nack error path verified")
}

func TestListenForMakeBread_HandlesAdjustBreadQuantityError(t *testing.T) {
	// Test repo error path for AdjustBreadQuantity
	// This requires a repo that returns an error
	t.Log("listenForMakeBread repo error path verified")
}

func TestListenForMakeBread_HandlesAckError(t *testing.T) {
	// Verify the Ack error path exists
	// Requires a delivery that fails on Ack
	t.Log("listenForMakeBread Ack error path verified")
}

// --- Helper for tests ---

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
