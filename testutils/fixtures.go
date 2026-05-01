package testutils

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v4/stdlib"
	log "github.com/sirupsen/logrus"
)

// IntegrationFixture holds all resources needed for integration tests
type IntegrationFixture struct {
	T           *testing.T
	DB          *sql.DB
	DBDSN       string
	ProjectDir  string
	ComposeFile string
	Cleanup     func()
}

// NewIntegrationFixture sets up the integration test environment.
// It starts postgres and rabbitmq via docker if they are not already running,
// then waits until both are healthy before returning.
func NewIntegrationFixture(t *testing.T) *IntegrationFixture {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Walk up to find the project root (where docker-compose.yml lives).
	projectDir := wd
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(projectDir, "docker-compose.yml")); err == nil {
			break
		}
		projectDir = filepath.Dir(projectDir)
	}

	fixture := &IntegrationFixture{
		T:           t,
		ProjectDir:  projectDir,
		ComposeFile: filepath.Join(projectDir, "docker-compose.yml"),
	}

	weStarted := false

	if !isContainerRunning("bakery-postgres") || !isContainerRunning("bakery-rabbitmq") {
		weStarted = true
		if err := startInfrastructure(projectDir); err != nil {
			t.Fatalf("Failed to start infrastructure containers: %v", err)
		}
	}

	// Wait for postgres to accept connections (up to 60 s).
	dsn := "postgres://postgres:password@localhost:5432/bakery?sslmode=disable"
	fixture.DBDSN = dsn

	db, err := waitForDB(dsn, 60*time.Second)
	if err != nil {
		t.Fatalf("Postgres not ready: %v", err)
	}
	fixture.DB = db

	// Wait for RabbitMQ AMQP port (up to 30 s).
	if err := waitForPort("localhost:5672", 30*time.Second); err != nil {
		t.Fatalf("RabbitMQ not ready: %v", err)
	}

	fixture.Cleanup = func() {
		if err := db.Close(); err != nil {
			log.Warnf("Failed to close database connection: %v", err)
		}
		// Do not stop containers: they are shared across all test packages and
		// must remain running for the full `go test ./...` run. Stopping them
		// here would wipe the DB between packages and force costly restarts.
		_ = weStarted
	}

	return fixture
}

// isContainerRunning returns true if a container with the given name is running.
func isContainerRunning(name string) bool {
	out, err := exec.Command("docker", "ps", "-q", "--filter", "name="+name, "--filter", "status=running").Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// startInfrastructure starts postgres and rabbitmq using docker run.
func startInfrastructure(projectDir string) error {
	sqlFile := filepath.Join(projectDir, "bakery.sql")
	seedFile := filepath.Join(projectDir, "seed-dev.sql")

	// Create the shared network if it does not exist yet.
	exec.Command("docker", "network", "create", "bakery-network").Run() //nolint:errcheck

	// --- PostgreSQL ---
	if !isContainerRunning("bakery-postgres") {
		// Remove a stopped container with the same name if it exists.
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
		log.Info("Started bakery-postgres")
	}

	// --- RabbitMQ ---
	if !isContainerRunning("bakery-rabbitmq") {
		exec.Command("docker", "rm", "-f", "bakery-rabbitmq").Run() //nolint:errcheck

		args := []string{
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
		log.Info("Started bakery-rabbitmq")
	}

	return nil
}

// stopInfrastructure tears down the containers that startInfrastructure created.
func stopInfrastructure() {
	for _, name := range []string{"bakery-postgres", "bakery-rabbitmq"} {
		exec.Command("docker", "rm", "-f", name).Run() //nolint:errcheck
	}
}

// waitForDB retries sql.Open + Ping until the database accepts connections or
// the timeout expires.
func waitForDB(dsn string, timeout time.Duration) (*sql.DB, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		db, err := sql.Open("pgx", dsn)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			err = db.PingContext(ctx)
			cancel()
			if err == nil {
				return db, nil
			}
			db.Close()
			lastErr = err
		} else {
			lastErr = err
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("timed out waiting for DB: %w", lastErr)
}

// waitForPort dials the TCP address until it succeeds or the timeout expires.
func waitForPort(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		out, err := exec.Command("docker", "exec", "bakery-rabbitmq",
			"rabbitmq-diagnostics", "-q", "ping").CombinedOutput()
		if err == nil {
			_ = out
			return nil
		}
		lastErr = fmt.Errorf("rabbitmq ping: %v", err)
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out waiting for %s: %w", addr, lastErr)
}

// GetDBDSNFromT returns the standard database DSN used by all integration tests.
func GetDBDSNFromT(t *testing.T) string {
	return "postgres://postgres:password@localhost:5432/bakery?sslmode=disable"
}

// GetGRPCAddress returns the gRPC server address used by integration tests.
func GetGRPCAddress() string {
	return "localhost:50051"
}

// GetRabbitMQAddress returns the RabbitMQ AMQP address used by integration tests.
func GetRabbitMQAddress() string {
	return "amqp://guest:guest@localhost:5672/"
}
