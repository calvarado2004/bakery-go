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
	T            *testing.T
	DB           *sql.DB
	DBDSN        string
	ProjectDir   string
	ComposeFile  string
	Cleanup      func()
}

// NewIntegrationFixture sets up the integration test environment using docker-compose
func NewIntegrationFixture(t *testing.T) *IntegrationFixture {
	// Get the project root directory
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	// Traverse up to find the project root (where docker-compose.yml is)
	projectDir := wd
	for i := 0; i < 5; i++ {
		composePath := filepath.Join(projectDir, "docker-compose.yml")
		if _, err := os.Stat(composePath); err == nil {
			break
		}
		projectDir = filepath.Dir(projectDir)
	}

	composeFile := filepath.Join(projectDir, "docker-compose.yml")

	// Check if containers are already running
	containersRunning := areContainersRunning(composeFile)

	fixture := &IntegrationFixture{
		T:           t,
		ProjectDir:  projectDir,
		ComposeFile: composeFile,
	}

	if !containersRunning {
		// Start services
		if err := startServices(composeFile); err != nil {
			t.Fatalf("Failed to start docker-compose services: %v", err)
		}
		// Wait for services to be ready
		time.Sleep(5 * time.Second)
	}

	// Connect to database
	dbDSN := getDBDSN(projectDir)
	fixture.DBDSN = dbDSN

	db, err := sql.Open("pgx", dbDSN)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("Failed to ping database: %v", err)
	}

	fixture.DB = db

	// Set up cleanup
	fixture.Cleanup = func() {
		if !containersRunning {
			// Only stop if we started them
			if err := stopServices(composeFile); err != nil {
				log.Warnf("Failed to stop docker-compose services: %v", err)
			}
		}
		if err := db.Close(); err != nil {
			log.Warnf("Failed to close database connection: %v", err)
		}
	}

	return fixture
}

// areContainersRunning checks if the docker-compose services are already running
func areContainersRunning(composeFile string) bool {
	cmd := exec.Command("docker-compose", "-f", composeFile, "ps", "-q")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(output))) > 0
}

// startServices starts all docker-compose services
func startServices(composeFile string) error {
	cmd := exec.Command("docker-compose", "-f", composeFile, "up", "-d")
	cmd.Dir = filepath.Dir(composeFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker-compose up failed: %v\n%s", err, string(output))
	}
	return nil
}

// stopServices stops all docker-compose services
func stopServices(composeFile string) error {
	cmd := exec.Command("docker-compose", "-f", composeFile, "down")
	cmd.Dir = filepath.Dir(composeFile)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker-compose down failed: %v\n%s", err, string(output))
	}
	return nil
}

// getDBDSN returns the database connection string
// Integration tests run from host machine and connect to docker-compose via localhost
func getDBDSN(projectDir string) string {
	return fmt.Sprintf("postgres://postgres:password@localhost:5432/bakery?sslmode=disable")
}

// GetDBDSNFromT returns the database connection string from a testing.T
func GetDBDSNFromT(t *testing.T) string {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get working directory: %v", err)
	}

	projectDir := wd
	for i := 0; i < 5; i++ {
		composePath := filepath.Join(projectDir, "docker-compose.yml")
		if _, err := os.Stat(composePath); err == nil {
			break
		}
		projectDir = filepath.Dir(projectDir)
	}

	return getDBDSN(projectDir)
}

// GetGRPCAddress returns the gRPC server address
// Integration tests connect to the server via localhost since docker exposes ports
func GetGRPCAddress() string {
	return "localhost:50051"
}

// GetRabbitMQAddress returns the RabbitMQ address
// Integration tests connect to RabbitMQ via localhost since docker exposes ports
func GetRabbitMQAddress() string {
	return "amqp://guest:guest@localhost:5672/"
}
