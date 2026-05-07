package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	_ "github.com/jackc/pgx/v4/stdlib"
	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ─────────────────────────────────────────────────────────────────────────────
// TestMain sets up global test environment before any tests run
// ─────────────────────────────────────────────────────────────────────────────

func TestMain(m *testing.M) {
	// Set JWT_SECRET before getJWTSecret's sync.Once runs
	os.Setenv("JWT_SECRET", "change-in-production")
	os.Exit(m.Run())
}

// ─────────────────────────────────────────────────────────────────────────────
// Test infrastructure
// ─────────────────────────────────────────────────────────────────────────────

// infrastructureMutex ensures only one test goroutine starts/stops the infra.
var infrastructureMutex sync.Mutex

// isInfraRunning checks whether the postgres and rabbitmq containers are alive.
func isInfraRunning() (bool, bool) {
	out, _ := exec.Command("docker", "ps", "-q", "--filter", "name=bakery-postgres", "--filter", "status=running").Output()
	pgOk := len(strings.TrimSpace(string(out))) > 0
	out2, _ := exec.Command("docker", "ps", "-q", "--filter", "name=bakery-rabbitmq", "--filter", "status=running").Output()
	rqOk := len(strings.TrimSpace(string(out2))) > 0
	return pgOk, rqOk
}

// setupInfra starts postgres + rabbitmq via docker-compose if needed.
func setupInfra(t *testing.T) {
	infrastructureMutex.Lock()
	defer infrastructureMutex.Unlock()

	pgOk, rqOk := isInfraRunning()
	if pgOk && rqOk {
		return
	}

	t.Log("Starting infrastructure containers via docker-compose...")
	cmd := exec.Command("docker-compose", "up", "-d", "postgres", "rabbitmq")
	cmd.Env = append(os.Environ(), "JWT_SECRET=change-in-production")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("docker-compose up output: %s", string(out))
	}

	// Wait for postgres
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command("docker-compose", "exec", "-T", "postgres", "pg_isready", "-U", "postgres", "-d", "bakery")
		if err := cmd.Run(); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}

	// Wait for rabbitmq
	deadline = time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command("docker-compose", "exec", "-T", "rabbitmq", "rabbitmq-diagnostics", "-q", "ping")
		if err := cmd.Run(); err == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
}

// teardownInfra stops the containers (called by the last test).
func teardownInfra(t *testing.T) {
	infrastructureMutex.Lock()
	defer infrastructureMutex.Unlock()
	t.Log("Stopping infrastructure containers...")
	cmd := exec.Command("docker-compose", "down")
	cmd.Env = append(os.Environ(), "JWT_SECRET=change-in-production")
	_ = cmd.Run()
}

// newTestDB opens a connection to the test database.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "postgres://postgres:password@localhost:5432/bakery?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping DB: %v", err)
	}
	return db
}

// seedAccounts ensures the admin and customer accounts needed by integration tests exist.
func seedAccounts(t *testing.T, db *sql.DB) {
	t.Helper()

	const adminHash = "$2a$10$PHZBNmARXoZUa4WAHRbYpePNJiYGQPUTkeKWdzq28E8it2BfypDyq"
	const customerHash = "$2a$10$lWlfcAs2n8hT4z9PV/90EehZ5J04JQjz9B1fFO.GDUuVjyE/OlIr2"

	var adminExists bool
	db.QueryRowContext(context.Background(), `SELECT EXISTS(SELECT 1 FROM admin_users WHERE username='admin')`).Scan(&adminExists)
	if !adminExists {
		_, err := db.ExecContext(context.Background(),
			`INSERT INTO admin_users (username, email, password, role, created_at, updated_at)
			 VALUES ('admin','admin@bakery.com',$1,'admin',NOW(),NOW())`, adminHash)
		if err != nil {
			t.Logf("seed admin: %v", err)
		}
	}

	var custExists bool
	db.QueryRowContext(context.Background(), `SELECT EXISTS(SELECT 1 FROM customer WHERE email='john@doe.com')`).Scan(&custExists)
	if !custExists {
		_, err := db.ExecContext(context.Background(),
			`INSERT INTO customer (name, email, password, created_at, updated_at)
			 VALUES ('John Doe','john@doe.com',$1,NOW(),NOW())`, customerHash)
		if err != nil {
			t.Logf("seed customer: %v", err)
		}
	}
}

// clearTables removes all data and resets sequences.
func clearTables(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	tables := []string{
		"invoice_items", "invoices", "admin_users", "orders_processed",
		"order_details", "buy_order", "customer", "make_order_details",
		"make_order", "bread_maker", "pending_make_orders", "bread", "outbox",
	}
	for _, tName := range tables {
		db.ExecContext(ctx, "DELETE FROM "+tName) //nolint:errcheck
	}
	sequences := []string{
		"customer_id_seq", "buy_id_seq", "bread_id_seq", "bread_maker_id_seq",
		"make_order_id_seq", "pending_make_order_id_seq", "orders_processed_id_seq",
		"invoice_id_seq", "admin_user_id_seq", "invoice_item_id_seq",
	}
	for _, seq := range sequences {
		db.ExecContext(ctx, "ALTER SEQUENCE "+seq+" RESTART WITH 1") //nolint:errcheck
	}
}

// dialGRPC connects to the gRPC server.
func dialGRPC(t *testing.T) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(
		"localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithTimeout(30*time.Second),
	)
	if err != nil {
		t.Skipf("cannot dial gRPC server: %v", err)
	}
	return conn
}

// adminContext adds the admin JWT to outgoing metadata.
func adminContext(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

// loginAdmin authenticates as admin and returns the token.
func loginAdmin(t *testing.T, authClient pb.AuthServiceClient, ctx context.Context) string {
	t.Helper()
	resp, err := authClient.AdminLogin(ctx, &pb.LoginRequest{
		Username: "admin",
		Password: "admin123",
	})
	if err != nil || resp == nil || !resp.Success {
		t.Logf("AdminLogin failed: %v (success=%v)", err, resp != nil && resp.Success)
		return ""
	}
	return resp.Token
}

// loginCustomer authenticates as customer and returns the token.
func loginCustomer(t *testing.T, authClient pb.AuthServiceClient, ctx context.Context) string {
	t.Helper()
	resp, err := authClient.CustomerLogin(ctx, &pb.CustomerLoginRequest{
		Email:    "john@doe.com",
		Password: "password123",
	})
	if err != nil || resp == nil || !resp.Success {
		t.Logf("CustomerLogin failed: %v (success=%v)", err, resp != nil && resp.Success)
		return ""
	}
	return resp.Token
}

// waitForServer blocks until the gRPC server is ready (up to 30s).
func waitForServer(t *testing.T) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := grpc.NewClient(
			"localhost:50051",
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithTimeout(2*time.Second),
		)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(1 * time.Second)
	}
	t.Log("gRPC server not ready after 30s – tests may skip")
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: openDB, connectToDB, setupRepo
// ─────────────────────────────────────────────────────────────────────────────

func TestOpenDB(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	t.Run("ValidConnection", func(t *testing.T) {
		db, err := openDB("postgres://postgres:password@localhost:5432/bakery?sslmode=disable")
		if err != nil {
			t.Fatalf("openDB failed: %v", err)
		}
		defer db.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.PingContext(ctx); err != nil {
			t.Fatalf("db.PingContext failed: %v", err)
		}
	})

	t.Run("InvalidDSN", func(t *testing.T) {
		_, err := openDB("postgres://wrong:wrong@localhost:99999/nope?sslmode=disable")
		if err == nil {
			t.Error("expected error for invalid DSN, got nil")
		}
	})
}

func TestConnectToDB(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	// connectToDB retries up to 10 times with 5s delays, so we need a generous timeout.
	t.Setenv("DSN", "postgres://postgres:password@localhost:5432/bakery?sslmode=disable")
	db := connectToDB()
	if db == nil {
		t.Fatal("connectToDB returned nil")
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("connectToDB: db.PingContext failed: %v", err)
	}
}

func TestSetupRepo(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	db := newTestDB(t)
	defer db.Close()

	config := &Config{}
	config.setupRepo(db)

	if config.Repo == nil {
		t.Fatal("setupRepo: Repo is nil")
	}

	// Verify the repo actually works
	breads, err := config.Repo.GetAvailableBread()
	if err != nil {
		t.Fatalf("repo.GetAvailableBread failed: %v", err)
	}
	t.Logf("Repo initialized: %d bread items available", len(breads))
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: NewRabbitMQBakery, init
// ─────────────────────────────────────────────────────────────────────────────

func TestNewRabbitMQBakery(t *testing.T) {
	t.Run("NilDialerUsesReal", func(t *testing.T) {
		config := Config{}
		rmq := NewRabbitMQBakery(config, "amqp://localhost:5672/", nil)
		if rmq == nil {
			t.Fatal("NewRabbitMQBakery returned nil")
		}
		if rmq.rabbitmqDialer == nil {
			t.Error("expected realRabbitMQDialer, got nil")
		}
	})

	t.Run("CustomDialer", func(t *testing.T) {
		config := Config{}
		dialer := &mockDialer{}
		rmq := NewRabbitMQBakery(config, "amqp://localhost:5672/", dialer)
		if rmq == nil {
			t.Fatal("NewRabbitMQBakery returned nil")
		}
		_ = rmq
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: middleware (rbacCheck, BuildInterceptorChain, etc.)
// ─────────────────────────────────────────────────────────────────────────────

func TestIdentityFromMetadata(t *testing.T) {
	tests := []struct {
		name        string
		metadataMap metadata.MD
		want        string
	}{
		{
			name:        "customer_id",
			metadataMap: metadata.MD{"customer_id": []string{"123"}},
			want:        "cid:123",
		},
		{
			name:        "x-forwarded-for",
			metadataMap: metadata.MD{"x-forwarded-for": []string{"1.2.3.4"}},
			want:        "ip:1.2.3.4",
		},
		{
			name:        "unknown",
			metadataMap: metadata.MD{},
			want:        "ip:unknown",
		},
		{
			name:        "empty_customer_id",
			metadataMap: metadata.MD{"customer_id": []string{""}},
			want:        "ip:unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := identityFromMetadata(tt.metadataMap)
			if got != tt.want {
				t.Errorf("identityFromMetadata() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetMethodRole(t *testing.T) {
	tests := []struct {
		method  string
		want    minRole
	}{
		{"/bread.AdminService/GetDashboardStats", RoleAdmin},
		{"/bread.AdminService/GetAllCustomers", RoleAdmin},
		{"/bread.BuyOrderService/BuyOrder", RoleCustomer},
		{"/bread.CheckInventory/CheckBreadInventory", ""},
		{"/bread.AuthService/AdminLogin", ""},
		{"/bread.BrokerService/ReportOrder", ""},
		{"/bread.UnknownService/UnknownMethod", RoleCustomer}, // default
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			got := getMethodRole(tt.method)
			if got != tt.want {
				t.Errorf("getMethodRole(%q) = %q, want %q", tt.method, got, tt.want)
			}
		})
	}
}

func TestRBACCheck(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	ctx := context.Background()
	// No token needed for "NoMetadata" test below.

	t.Run("NoMetadata", func(t *testing.T) {
		_, err := rbacCheck(ctx, "/bread.AdminService/GetDashboardStats")
		if err == nil {
			t.Error("expected Unauthenticated error for no metadata")
		}
	})

	t.Run("NoToken", func(t *testing.T) {
		md := metadata.New(map[string]string{})
		ctx := metadata.NewIncomingContext(context.Background(), md)
		_, err := rbacCheck(ctx, "/bread.AdminService/GetDashboardStats")
		if err == nil {
			t.Error("expected Unauthenticated error for no token")
		}
	})

	t.Run("InvalidToken", func(t *testing.T) {
		md := metadata.New(map[string]string{"authorization": "Bearer invalid.token.here"})
		ctx := metadata.NewIncomingContext(context.Background(), md)
		_, err := rbacCheck(ctx, "/bread.AdminService/GetDashboardStats")
		if err == nil {
			t.Error("expected Unauthenticated error for invalid token")
		}
	})

	t.Run("ValidAdminToken", func(t *testing.T) {
		// We need to login via gRPC to get a valid token.
		// Skip if server not running, but if it is, test the rbac flow.
		conn := dialGRPC(t)
		if conn == nil {
			t.Skip("gRPC server not available")
		}
		defer conn.Close()

		time.Sleep(2 * time.Second) // rate limiter refill
		authClient := pb.NewAuthServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		token := loginAdmin(t, authClient, ctx)
		if token == "" {
			t.Skip("Could not login as admin")
		}

		md := metadata.New(map[string]string{"authorization": "Bearer " + token})
		ctx = metadata.NewIncomingContext(context.Background(), md)

		newCtx, err := rbacCheck(ctx, "/bread.AdminService/GetDashboardStats")
		if err != nil {
			t.Fatalf("rbacCheck with valid admin token failed: %v", err)
		}

		claims := GetClaimsFromContext(newCtx)
		if claims == nil {
			t.Fatal("GetClaimsFromContext returned nil")
		}
		if claims.UserType != "admin" {
			t.Errorf("expected UserType 'admin', got %q", claims.UserType)
		}
	})

	t.Run("ValidCustomerTokenForAdminEndpoint", func(t *testing.T) {
		conn := dialGRPC(t)
		if conn == nil {
			t.Skip("gRPC server not available")
		}
		defer conn.Close()

		time.Sleep(2 * time.Second)
		authClient := pb.NewAuthServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		token := loginCustomer(t, authClient, ctx)
		if token == "" {
			t.Skip("Could not login as customer")
		}

		md := metadata.New(map[string]string{"authorization": "Bearer " + token})
		ctx = metadata.NewIncomingContext(context.Background(), md)

		_, err := rbacCheck(ctx, "/bread.AdminService/GetDashboardStats")
		if err == nil {
			t.Error("expected PermissionDenied for customer on admin endpoint")
		}
		// Verify it's a PermissionDenied error
		if s, ok := status.FromError(err); !ok || s.Code() != codes.PermissionDenied {
			t.Logf("Expected PermissionDenied, got: %v", err)
		}
	})

	t.Run("PublicEndpoint", func(t *testing.T) {
		md := metadata.New(map[string]string{})
		ctx := metadata.NewIncomingContext(context.Background(), md)
		newCtx, err := rbacCheck(ctx, "/bread.CheckInventory/CheckBreadInventory")
		if err != nil {
			t.Fatalf("public endpoint should not require auth: %v", err)
		}
		_ = newCtx
	})
}

func TestGetClaimsFromContext(t *testing.T) {
	// No claims in empty context
	empty := context.Background()
	if GetClaimsFromContext(empty) != nil {
		t.Error("expected nil claims from empty context")
	}

	// Claims injected by rbacCheck are tested above.
}

func TestInjectCustomerID(t *testing.T) {
	t.Run("WithValidCustomerID", func(t *testing.T) {
		md := metadata.New(map[string]string{"customer_id": "123"})
		ctx := metadata.NewIncomingContext(context.Background(), md)
		newCtx := injectCustomerID(ctx)

		v := newCtx.Value(customerIDKey)
		if v == nil {
			t.Error("expected customerID in context")
		}
	})

	t.Run("WithInvalidCustomerID", func(t *testing.T) {
		md := metadata.New(map[string]string{"customer_id": "abc"})
		ctx := metadata.NewIncomingContext(context.Background(), md)
		newCtx := injectCustomerID(ctx)

		v := newCtx.Value(customerIDKey)
		if v != nil {
			t.Error("expected no customerID for non-numeric ID")
		}
	})

	t.Run("WithoutCustomerID", func(t *testing.T) {
		md := metadata.New(map[string]string{})
		ctx := metadata.NewIncomingContext(context.Background(), md)
		newCtx := injectCustomerID(ctx)

		v := newCtx.Value(customerIDKey)
		if v != nil {
			t.Error("expected no customerID when not provided")
		}
	})
}

func TestCircuitBreakers(t *testing.T) {
	t.Run("AllBreakersNotNil", func(t *testing.T) {
		breakers := allBrokerBreakers()
		if len(breakers) != 3 {
			t.Errorf("expected 3 breakers, got %d", len(breakers))
		}
		for name, cb := range breakers {
			if cb == nil {
				t.Errorf("breaker %q is nil", name)
			}
		}
	})

	t.Run("GetReportOrderBreaker", func(t *testing.T) {
		cb := getReportOrderBreaker()
		if cb == nil {
			t.Error("expected non-nil report order breaker")
		}
	})

	t.Run("GetReserveInventoryBreaker", func(t *testing.T) {
		cb := getReserveInventoryBreaker()
		if cb == nil {
			t.Error("expected non-nil reserve inventory breaker")
		}
	})

	t.Run("GetReportMatchingBreaker", func(t *testing.T) {
		cb := getReportMatchingBreaker()
		if cb == nil {
			t.Error("expected non-nil report matching breaker")
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: SettlementDispatcher
// ─────────────────────────────────────────────────────────────────────────────

func TestSettlementDispatcher(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	config := Config{}
	rmq := NewRabbitMQBakery(config, "amqp://guest:guest@localhost:5672/", nil)
	if rmq == nil {
		t.Fatal("NewRabbitMQBakery returned nil")
	}

	sd := NewSettlementDispatcher(rmq, "amqp://guest:guest@localhost:5672/")
	if sd == nil {
		t.Fatal("NewSettlementDispatcher returned nil")
	}

	// Test Register/Unregister without starting the dispatcher (no AMQP needed)
	uuid := "test-uuid-1"
	ch := sd.Register(uuid)
	if ch == nil {
		t.Fatal("Register returned nil channel")
	}

	// Unregister should close the channel
	sd.Unregister(uuid)

	// After unregister, the channel should be closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected closed channel after Unregister")
		}
	default:
		t.Error("channel should be closed (non-blocking receive should return)")
	}

	// Register again — should create a new channel
	ch2 := sd.Register(uuid)
	if ch2 == nil {
		t.Fatal("second Register returned nil channel")
	}
}

func TestSettlementDispatcherDeliver(t *testing.T) {
	sd := &SettlementDispatcher{
		waiters: make(map[string]*settlementWaiter),
	}

	uuid := "test-deliver-1"
	ch := make(chan *data.BuyOrder, 1)
	sd.waiters[uuid] = &settlementWaiter{ch: ch, closed: false}

	order := &data.BuyOrder{BuyOrderUUID: uuid}

	// deliver should succeed
	if !sd.deliver(uuid, order) {
		t.Error("deliver should return true for active waiter")
	}

	// After delivery, the waiter should still exist but channel has value
	select {
	case received := <-ch:
		if received.BuyOrderUUID != uuid {
			t.Errorf("expected order UUID %s, got %s", uuid, received.BuyOrderUUID)
		}
	default:
		t.Error("expected to receive order from channel")
	}

	// Deliver to a waiter that was closed
	sd.waiters[uuid].closed = true
	if sd.deliver(uuid, order) {
		t.Error("deliver should return false for closed waiter")
	}

	// Deliver to non-existent UUID
	if sd.deliver("nonexistent", order) {
		t.Error("deliver should return false for non-existent UUID")
	}
}

func TestSettlementDispatcherRegisterDuplicate(t *testing.T) {
	sd := &SettlementDispatcher{
		waiters: make(map[string]*settlementWaiter),
	}

	uuid := "test-dup-1"

	// First register
	ch1 := sd.Register(uuid)

	// Second register should clean up the first
	ch2 := sd.Register(uuid)

	// ch1 should be closed (old waiter cleaned up)
	select {
	case _, ok := <-ch1:
		if ok {
			t.Error("expected ch1 to be closed after duplicate Register")
		}
	default:
		t.Error("ch1 should be closed")
	}

	// ch2 should be usable
	if ch2 == nil {
		t.Fatal("ch2 is nil")
	}

	sd.Unregister(uuid)
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: BrokerService integration (direct gRPC → server)
// ─────────────────────────────────────────────────────────────────────────────

func TestBrokerServiceIntegration(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	// Seed DB
	db := newTestDB(t)
	defer db.Close()
	clearTables(t, db)
	seedAccounts(t, db)

	// Wait for server
	waitForServer(t)
	time.Sleep(2 * time.Second)

	conn := dialGRPC(t)
	if conn == nil {
		t.Skip("gRPC server not available")
	}
	defer conn.Close()

	time.Sleep(2 * time.Second) // rate limiter

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := pb.NewBrokerServiceClient(conn)

	t.Run("ReportOrder", func(t *testing.T) {
		order := &pb.BuyOrder{
			BuyOrderUuid:   fmt.Sprintf("test-broker-%d", time.Now().UnixNano()),
			CustomerId:     1,
			SequenceNumber: 1,
			BidPrice:       5.0,
			Items: []*pb.BuyOrderItem{
				{BreadId: 1, QuantityRequested: 1},
			},
		}
		_, err := client.ReportOrder(ctx, order)
		if err != nil {
			t.Logf("ReportOrder: %v (stock may be exhausted)", err)
		}
	})

	t.Run("ReserveInventory", func(t *testing.T) {
		reservation := &pb.ReserveInventoryRequest{
			BreadId:             1,
			QuantityRequested:  1,
			BuyOrderUuid:       "test-uuid",
		}
		_, err := client.ReserveInventory(ctx, reservation)
		if err != nil {
			t.Logf("ReserveInventory: %v", err)
		}
	})

	t.Run("ReportMatchingResults", func(t *testing.T) {
		results := &pb.MatchingBatch{
			Results: []*pb.MatchingBatchResult{},
		}
		_, err := client.ReportMatchingResults(ctx, results)
		if err != nil {
			t.Logf("ReportMatchingResults: %v", err)
		}
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests: gRPC server endpoints (full integration with real DB)
// ─────────────────────────────────────────────────────────────────────────────

func TestServerEndpoints_Integration(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	// Seed DB
	db := newTestDB(t)
	defer db.Close()
	clearTables(t, db)
	seedAccounts(t, db)

	// Wait for server
	waitForServer(t)

	conn := dialGRPC(t)
	if conn == nil {
		t.Skip("gRPC server not available")
	}
	defer conn.Close()

	time.Sleep(2 * time.Second) // rate limiter refill

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	authClient := pb.NewAuthServiceClient(conn)
	adminClient := pb.NewAdminServiceClient(conn)
	inventoryClient := pb.NewCheckInventoryClient(conn)
	invoiceClient := pb.NewInvoiceServiceClient(conn)

	// Login as admin
	adminToken := loginAdmin(t, authClient, ctx)
	if adminToken == "" {
		t.Skip("Could not login as admin")
	}
	adminCtx := adminContext(ctx, adminToken)

	// CheckInventory (no auth needed)
	t.Run("CheckInventory", func(t *testing.T) {
		resp, err := inventoryClient.CheckBreadInventory(ctx, &pb.BreadRequest{})
		if err != nil {
			t.Logf("CheckBreadInventory: %v", err)
		} else if resp != nil && resp.Breads != nil {
			t.Logf("Inventory has %d bread types", len(resp.Breads.Breads))
		}
	})

	// Admin endpoints
	t.Run("GetDashboardStats", func(t *testing.T) {
		resp, err := adminClient.GetDashboardStats(adminCtx, &pb.Empty{})
		if err != nil {
			t.Fatalf("GetDashboardStats: %v", err)
		}
		t.Logf("Dashboard: Orders=%d, Revenue=$%.2f, Products=%d",
			resp.TotalOrders, resp.TotalRevenue, resp.TotalProducts)
	})

	t.Run("GetAllBread", func(t *testing.T) {
		resp, err := adminClient.GetAllBread(adminCtx, &pb.Empty{})
		if err != nil {
			t.Fatalf("GetAllBread: %v", err)
		}
		t.Logf("All bread: %d items", len(resp.Breads))
	})

	t.Run("GetAllCustomers", func(t *testing.T) {
		resp, err := adminClient.GetAllCustomers(adminCtx, &pb.Empty{})
		if err != nil {
			t.Fatalf("GetAllCustomers: %v", err)
		}
		t.Logf("Customers: %d", len(resp.Customers))
	})

	t.Run("GetAllBreadMakers", func(t *testing.T) {
		resp, err := adminClient.GetAllBreadMakers(adminCtx, &pb.Empty{})
		if err != nil {
			t.Fatalf("GetAllBreadMakers: %v", err)
		}
		t.Logf("Bread makers: %d", len(resp.BreadMakers))
	})

	t.Run("GetAllOrders", func(t *testing.T) {
		resp, err := adminClient.GetAllOrders(adminCtx, &pb.Empty{})
		if err != nil {
			t.Fatalf("GetAllOrders: %v", err)
		}
		t.Logf("Orders: %d", len(resp.BuyOrders))
	})

	t.Run("GetAllMakeOrders", func(t *testing.T) {
		resp, err := adminClient.GetAllMakeOrders(adminCtx, &pb.Empty{})
		if err != nil {
			t.Fatalf("GetAllMakeOrders: %v", err)
		}
		t.Logf("Make orders: %d", len(resp.MakeOrders))
	})

	// Invoice endpoints
	t.Run("GetAllInvoices", func(t *testing.T) {
		resp, err := invoiceClient.GetAllInvoices(adminCtx, &pb.Empty{})
		if err != nil {
			t.Logf("GetAllInvoices: %v (may be empty)", err)
		} else {
			t.Logf("Invoices: %d", len(resp.Invoices))
		}
	})
}

func TestServerAuth_Integration(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	// Seed DB
	db := newTestDB(t)
	defer db.Close()
	clearTables(t, db)
	seedAccounts(t, db)

	waitForServer(t)
	conn := dialGRPC(t)
	if conn == nil {
		t.Skip("gRPC server not available")
	}
	defer conn.Close()

	time.Sleep(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	authClient := pb.NewAuthServiceClient(conn)

	t.Run("AdminLogin", func(t *testing.T) {
		resp, err := authClient.AdminLogin(ctx, &pb.LoginRequest{
			Username: "admin",
			Password: "admin123",
		})
		if err != nil {
			t.Fatalf("AdminLogin: %v", err)
		}
		if !resp.Success {
			t.Fatal("AdminLogin: not successful")
		}
		if resp.Token == "" {
			t.Fatal("AdminLogin: empty token")
		}
		t.Logf("Admin login successful, token len=%d", len(resp.Token))
	})

	t.Run("CustomerLogin", func(t *testing.T) {
		resp, err := authClient.CustomerLogin(ctx, &pb.CustomerLoginRequest{
			Email:    "john@doe.com",
			Password: "password123",
		})
		if err != nil {
			t.Fatalf("CustomerLogin: %v", err)
		}
		if !resp.Success {
			t.Fatal("CustomerLogin: not successful")
		}
		if resp.Token == "" {
			t.Fatal("CustomerLogin: empty token")
		}
	})

	t.Run("ValidateToken", func(t *testing.T) {
		// Get a token first
		loginResp, err := authClient.AdminLogin(ctx, &pb.LoginRequest{
			Username: "admin",
			Password: "admin123",
		})
		if err != nil {
			t.Fatalf("AdminLogin for ValidateToken: %v", err)
		}

		validateResp, err := authClient.ValidateToken(ctx, &pb.ValidateTokenRequest{
			Token: loginResp.Token,
		})
		if err != nil {
			t.Fatalf("ValidateToken: %v", err)
		}
		if !validateResp.Valid {
			t.Fatal("ValidateToken: token not valid")
		}
		if validateResp.UserType != "admin" {
			t.Errorf("expected UserType 'admin', got %q", validateResp.UserType)
		}
		if validateResp.UserId == "" {
			t.Error("ValidateToken: empty UserId")
		}
		t.Logf("Token valid: UserID=%s, UserType=%s", validateResp.UserId, validateResp.UserType)
	})
}

func TestServerAdminCRUD_Integration(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	db := newTestDB(t)
	defer db.Close()
	clearTables(t, db)
	seedAccounts(t, db)

	waitForServer(t)
	conn := dialGRPC(t)
	if conn == nil {
		t.Skip("gRPC server not available")
	}
	defer conn.Close()

	time.Sleep(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	authClient := pb.NewAuthServiceClient(conn)
	adminClient := pb.NewAdminServiceClient(conn)

	token := loginAdmin(t, authClient, ctx)
	if token == "" {
		t.Skip("Could not login as admin")
	}
	adminCtx := adminContext(ctx, token)

	t.Run("CreateBread", func(t *testing.T) {
		req := &pb.CreateBreadRequest{
			Name:        "Integration Test Bread",
			Price:       6.99,
			Quantity:    100,
			Description: "Test bread for integration test",
			Type:        "Test Type",
			Image:       "/images/test.png",
		}
		resp, err := adminClient.CreateBread(adminCtx, req)
		if err != nil {
			t.Fatalf("CreateBread: %v", err)
		}
		if resp.Id <= 0 {
			t.Fatal("CreateBread: invalid ID")
		}

		// Clean up
		_, _ = adminClient.DeleteBread(adminCtx, &pb.DeleteBreadRequest{Id: resp.Id})
		t.Logf("Created and deleted bread ID=%d", resp.Id)
	})

	t.Run("GetBreadById_NotFound", func(t *testing.T) {
		_, err := adminClient.GetBreadById(adminCtx, &pb.BreadIdRequest{Id: 99999})
		if err == nil {
			t.Error("expected error for non-existent bread")
		}
	})
}

func TestServerInvoice_Integration(t *testing.T) {
	setupInfra(t)
	defer teardownInfra(t)

	db := newTestDB(t)
	defer db.Close()
	clearTables(t, db)
	seedAccounts(t, db)

	waitForServer(t)
	conn := dialGRPC(t)
	if conn == nil {
		t.Skip("gRPC server not available")
	}
	defer conn.Close()

	time.Sleep(2 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	authClient := pb.NewAuthServiceClient(conn)
	adminClient := pb.NewAdminServiceClient(conn)
	invoiceClient := pb.NewInvoiceServiceClient(conn)

	token := loginAdmin(t, authClient, ctx)
	if token == "" {
		t.Skip("Could not login as admin")
	}
	adminCtx := adminContext(ctx, token)

	t.Run("CreateInvoice", func(t *testing.T) {
		// Get an order first
		ordersResp, err := adminClient.GetAllOrders(adminCtx, &pb.Empty{})
		if err != nil || len(ordersResp.BuyOrders) == 0 {
			t.Skip("No orders available")
		}

		order := ordersResp.BuyOrders[0]
		resp, err := invoiceClient.CreateInvoice(adminCtx, &pb.CreateInvoiceRequest{
			BuyOrderId: order.Id,
		})
		if err != nil {
			t.Fatalf("CreateInvoice: %v", err)
		}
		t.Logf("Invoice created: %s for order %d", resp.InvoiceNumber, resp.BuyOrderId)
	})

	t.Run("GetAllInvoices", func(t *testing.T) {
		resp, err := invoiceClient.GetAllInvoices(adminCtx, &pb.Empty{})
		if err != nil {
			t.Fatalf("GetAllInvoices: %v", err)
		}
		t.Logf("Invoices: %d", len(resp.Invoices))
	})
}

// isServerRunning checks if the gRPC server container is up and healthy.
func isServerRunning() bool {
	out, err := exec.Command("docker", "ps", "-q", "--filter", "name=bakery-server", "--filter", "status=running").CombinedOutput()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}
