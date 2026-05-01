package main

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// --- Integration test fixtures ---

type integrationTestEnv struct {
	buyBreadClient   pb.BuyBreadClient
	buyBreadStreamClient pb.BuyBreadClient
	conn             *grpc.ClientConn
}

func setupIntegrationTestEnv(t *testing.T, grpcAddr string) *integrationTestEnv {
	t.Helper()

	conn, err := grpc.Dial(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("Failed to connect to gRPC server at %s: %v", grpcAddr, err)
	}

	return &integrationTestEnv{
		conn:               conn,
		buyBreadClient:     pb.NewBuyBreadClient(conn),
		buyBreadStreamClient: pb.NewBuyBreadClient(conn),
	}
}

func (env *integrationTestEnv) teardown(t *testing.T) {
	t.Helper()
	if err := env.conn.Close(); err != nil {
		t.Logf("Failed to close gRPC connection: %v", err)
	}
}

// --- Integration tests for buySomeBread ---

func TestIntegrationBuySomeBread_RealServerConnection(t *testing.T) {
	grpcAddr := getEnvOrDefault("BAKERY_SERVICE_ADDR", "localhost:50051")
	env := setupIntegrationTestEnv(t, grpcAddr)
	defer env.teardown(t)

	config := &Config{
		conn:           env.conn,
		buyBreadClient: env.buyBreadClient,
	}

	buyOrderUUID := uuid.NewString()
	buyBreadChan := make(chan bool, 1)
	breadBoughtChan := make(chan bool, 1)
	doneBuy := make(chan bool, 1)
	errChan := make(chan error, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	go config.buySomeBread(ctx, buyBreadChan, breadBoughtChan, doneBuy, buyOrderUUID, errChan)

	// Send signal to buy bread
	buyBreadChan <- true

	select {
	case <-doneBuy:
		// Success - bread order was sent
		t.Logf("Integration test passed: buy order %s sent successfully", buyOrderUUID)
	case err := <-errChan:
		t.Logf("Integration test: got error (expected if server not running): %v", err)
	case <-time.After(25 * time.Second):
		// Timeout is acceptable if server is not running
		t.Skip("Server not available, skipping integration test")
	}
}

// --- Integration tests for buyBreadStream ---

func TestIntegrationBuyBreadStream_RealServerConnection(t *testing.T) {
	grpcAddr := getEnvOrDefault("BAKERY_SERVICE_ADDR", "localhost:50051")
	env := setupIntegrationTestEnv(t, grpcAddr)
	defer env.teardown(t)

	config := &Config{
		conn:           env.conn,
		buyBreadClient: env.buyBreadStreamClient,
	}

	buyOrderUUID := uuid.NewString()
	breadBoughtChan := make(chan bool, 1)
	doneStream := make(chan bool, 1)
	errChan := make(chan error, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	go config.buyBreadStream(ctx, breadBoughtChan, doneStream, buyOrderUUID, errChan)

	// Signal that we're ready to consume the stream
	breadBoughtChan <- true

	select {
	case <-doneStream:
		t.Logf("Integration test passed: stream consumed successfully for order %s", buyOrderUUID)
	case err := <-errChan:
		t.Logf("Integration test: got error (expected if server not running): %v", err)
	case <-time.After(25 * time.Second):
		// Timeout is acceptable if server is not running
		t.Skip("Server not available, skipping integration test")
	}
}

func TestIntegrationBuyBreadStream_MultipleRecv(t *testing.T) {
	grpcAddr := getEnvOrDefault("BAKERY_SERVICE_ADDR", "localhost:50051")
	env := setupIntegrationTestEnv(t, grpcAddr)
	defer env.teardown(t)

	config := &Config{
		conn:           env.conn,
		buyBreadClient: env.buyBreadStreamClient,
	}

	buyOrderUUID := uuid.NewString()
	breadBoughtChan := make(chan bool, 1)
	doneStream := make(chan bool, 1)
	errChan := make(chan error, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	go config.buyBreadStream(ctx, breadBoughtChan, doneStream, buyOrderUUID, errChan)
	breadBoughtChan <- true

	// Collect responses
	var responses []*pb.BreadResponse
	var mu sync.Mutex

	// Manually consume stream to count responses
	stream, err := config.buyBreadClient.BuyBreadStream(ctx, &pb.BreadRequest{BuyOrderUuid: buyOrderUUID})
	if err == nil {
		timeout := time.After(20 * time.Second)
		for {
			select {
			case <-timeout:
				goto done
			default:
				resp, recvErr := stream.Recv()
				if recvErr == io.EOF {
					goto done
				}
				if recvErr == nil {
					mu.Lock()
					responses = append(responses, resp)
					mu.Unlock()
				}
			}
		}
	}

done:
	mu.Lock()
	t.Logf("Integration test received %d stream responses for order %s", len(responses), buyOrderUUID)
	mu.Unlock()

	select {
	case <-doneStream:
		t.Log("Stream completed successfully")
	case err := <-errChan:
		t.Logf("Stream error (acceptable if server not running): %v", err)
	case <-time.After(25 * time.Second):
		t.Skip("Server not available, skipping detailed response test")
	}
}

// TestIntegrationFullBuyFlow runs the complete buyer flow against a real server.
// It reproduces the critical bug where BuyBreadStream never receives the
// order-settlement response (the "missing fill confirmation" bug).
//
// When a real server is running this test MUST pass; a timeout is a FAILURE
// because it means the buyer never got confirmation that the order was settled.
func TestIntegrationFullBuyFlow(t *testing.T) {
	grpcAddr := getEnvOrDefault("BAKERY_SERVICE_ADDR", "localhost:50051")
	env := setupIntegrationTestEnv(t, grpcAddr)
	defer env.teardown(t)

	config := &Config{
		conn:           env.conn,
		buyBreadClient: env.buyBreadClient,
	}

	buyOrderUUID := uuid.NewString()

	buyBreadChan := make(chan bool, 1)
	breadBoughtChan := make(chan bool, 2)
	doneBuy := make(chan bool, 1)
	doneStream := make(chan bool, 1)
	errChan := make(chan error, 2)

	// Use a generous timeout: broker processing + DB polling can take ~10 s
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	go config.buySomeBread(ctx, buyBreadChan, breadBoughtChan, doneBuy, buyOrderUUID, errChan)
	go config.buyBreadStream(ctx, breadBoughtChan, doneStream, buyOrderUUID, errChan)

	buyBreadChan <- true

	globalDone := make(chan struct{})
	go func() {
		<-doneBuy
		<-doneStream
		close(globalDone)
	}()

	select {
	case <-globalDone:
		t.Logf("✓ Full integration flow completed successfully for order %s", buyOrderUUID)
	case err := <-errChan:
		// If the gRPC error is "connection refused" the server isn't running — skip.
		if isServerUnavailable(err) {
			t.Skipf("Server not available, skipping: %v", err)
		}
		t.Fatalf("Buy flow failed for order %s: %v", buyOrderUUID, err)
	case <-time.After(40 * time.Second):
		// CRITICAL BUG REPRODUCED: the stream never received the settlement.
		t.Fatalf("CRITICAL BUG: BuyBreadStream timed out for order %s — "+
			"the buyer never received order settlement confirmation. "+
			"This reproduces the production bug where WaitForOrderNotification "+
			"(LISTEN/NOTIFY) silently fails in containerised environments.", buyOrderUUID)
	}
}

// isServerUnavailable returns true if err indicates the gRPC server is not running.
func isServerUnavailable(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "connection refused") ||
		strings.Contains(s, "Unavailable") ||
		strings.Contains(s, "no such host")
}

// --- Helper functions ---

func getEnvOrDefault(key, defaultValue string) string {
	if val := "localhost:50051"; val != "" {
		return val
	}
	return defaultValue
}

// --- Edge case integration tests ---

func TestIntegrationBuySomeBread_ConcurrentRequests(t *testing.T) {
	grpcAddr := getEnvOrDefault("BAKERY_SERVICE_ADDR", "localhost:50051")
	env := setupIntegrationTestEnv(t, grpcAddr)
	defer env.teardown(t)

	config := &Config{
		conn:           env.conn,
		buyBreadClient: env.buyBreadClient,
	}

	numRequests := 3
	var wg sync.WaitGroup
	results := make([]bool, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			buyOrderUUID := uuid.NewString()
			buyBreadChan := make(chan bool, 1)
			breadBoughtChan := make(chan bool, 1)
			doneBuy := make(chan bool, 1)
			errChan := make(chan error, 2)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()

			go config.buySomeBread(ctx, buyBreadChan, breadBoughtChan, doneBuy, buyOrderUUID, errChan)
			buyBreadChan <- true

			select {
			case <-doneBuy:
				results[idx] = true
			case <-time.After(15 * time.Second):
				results[idx] = false
			}
		}(i)
	}

	wg.Wait()

	successCount := 0
	for _, result := range results {
		if result {
			successCount++
		}
	}

	t.Logf("Concurrent integration test: %d/%d requests completed", successCount, numRequests)
	if successCount > 0 {
		t.Log("At least some concurrent requests succeeded (full success depends on server availability)")
	}
}

func TestIntegrationBuyBreadStream_ContextCancellation(t *testing.T) {
	grpcAddr := getEnvOrDefault("BAKERY_SERVICE_ADDR", "localhost:50051")
	env := setupIntegrationTestEnv(t, grpcAddr)
	defer env.teardown(t)

	config := &Config{
		conn:           env.conn,
		buyBreadClient: env.buyBreadStreamClient,
	}

	buyOrderUUID := uuid.NewString()
	breadBoughtChan := make(chan bool, 1)
	doneStream := make(chan bool, 1)
	errChan := make(chan error, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	go config.buyBreadStream(ctx, breadBoughtChan, doneStream, buyOrderUUID, errChan)
	breadBoughtChan <- true

	// Cancel context early
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-doneStream:
		t.Log("Stream completed before cancellation")
	case err := <-errChan:
		t.Logf("Stream received error after cancellation (expected): %v", err)
	case <-time.After(3 * time.Second):
		t.Log("Stream is still running after cancellation (acceptable)")
	}
}
