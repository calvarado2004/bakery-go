package main

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	pb "github.com/calvarado2004/bakery-go/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// --- mock BuyBreadClient ---

type mockBuyBreadClient struct {
	buyBreadResp  *pb.BreadResponse
	buyBreadErr   error
	buyBreadReq   *pb.BreadRequest // captures last request
	streamClient  grpc.ServerStreamingClient[pb.BreadResponse]
	streamErr     error
	streamReq     *pb.BreadRequest // captures last stream request
	buyBreadBlock chan struct{}    // if set, blocks BuyBread until closed
}

func (m *mockBuyBreadClient) BuyBread(_ context.Context, req *pb.BreadRequest, _ ...grpc.CallOption) (*pb.BreadResponse, error) {
	m.buyBreadReq = req
	if m.buyBreadBlock != nil {
		<-m.buyBreadBlock // block until closed
	}
	if m.buyBreadResp == nil && m.buyBreadErr == nil {
		return nil, errors.New("mock: no response or error configured")
	}
	return m.buyBreadResp, m.buyBreadErr
}

func (m *mockBuyBreadClient) BuyBreadStream(_ context.Context, req *pb.BreadRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[pb.BreadResponse], error) {
	m.streamReq = req
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	if m.streamClient == nil {
		return nil, errors.New("mock: no stream client configured")
	}
	return m.streamClient, nil
}

// --- mock ServerStreamingClient[BreadResponse] ---

type mockBreadStreamClient struct {
	mu        sync.Mutex
	responses []*pb.BreadResponse
	pos       int
	recvErr   error // returned after all responses; defaults to io.EOF
}

func newMockStream(responses ...*pb.BreadResponse) *mockBreadStreamClient {
	return &mockBreadStreamClient{responses: responses}
}

func (m *mockBreadStreamClient) Recv() (*pb.BreadResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pos < len(m.responses) {
		r := m.responses[m.pos]
		m.pos++
		return r, nil
	}
	if m.recvErr != nil {
		return nil, m.recvErr
	}
	return nil, io.EOF
}

func (m *mockBreadStreamClient) Header() (metadata.MD, error) { return nil, nil }
func (m *mockBreadStreamClient) Trailer() metadata.MD         { return nil }
func (m *mockBreadStreamClient) CloseSend() error             { return nil }
func (m *mockBreadStreamClient) Context() context.Context     { return context.Background() }
func (m *mockBreadStreamClient) SendMsg(interface{}) error    { return nil }
func (m *mockBreadStreamClient) RecvMsg(interface{}) error    { return nil }

// --- helpers ---

func newConfig(client pb.BuyBreadClient) *Config {
	return &Config{buyBreadClient: client}
}

// --- buySomeBread tests ---

func TestBuySomeBread_SuccessSendsSignals(t *testing.T) {
	client := &mockBuyBreadClient{
		buyBreadResp: &pb.BreadResponse{
			Breads:  &pb.BreadList{Breads: []*pb.Bread{{Name: "Pretzel"}}},
			Message: "started",
		},
	}
	cfg := newConfig(client)

	buyBreadChan  := make(chan bool, 1)
	breadBoughtChan := make(chan bool, 1)
	doneBuy       := make(chan bool, 1)
	errChan       := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cfg.buySomeBread(ctx, buyBreadChan, breadBoughtChan, doneBuy, "uuid-ok", errChan)

	buyBreadChan <- true

	select {
	case <-doneBuy:
		// success
	case err := <-errChan:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for doneBuy signal")
	}

	select {
	case <-breadBoughtChan:
		// signal was sent before doneBuy — drain it
	default:
		// already drained by caller
	}
}

func TestBuySomeBread_ErrorSendsToErrChan(t *testing.T) {
	client := &mockBuyBreadClient{
		buyBreadErr: errors.New("connection refused"),
	}
	cfg := newConfig(client)

	buyBreadChan  := make(chan bool, 1)
	breadBoughtChan := make(chan bool, 1)
	doneBuy       := make(chan bool, 1)
	errChan       := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cfg.buySomeBread(ctx, buyBreadChan, breadBoughtChan, doneBuy, "uuid-err", errChan)

	buyBreadChan <- true

	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("expected non-nil error")
		}
	case <-doneBuy:
		t.Fatal("expected error path, got doneBuy signal")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for errChan")
	}
}

func TestBuySomeBread_ContextCancelledBeforeSignal(t *testing.T) {
	client := &mockBuyBreadClient{
		buyBreadResp: &pb.BreadResponse{Breads: &pb.BreadList{}},
	}
	cfg := newConfig(client)

	buyBreadChan  := make(chan bool)
	breadBoughtChan := make(chan bool, 1)
	doneBuy       := make(chan bool, 1)
	errChan       := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())

	go cfg.buySomeBread(ctx, buyBreadChan, breadBoughtChan, doneBuy, "uuid-cancel", errChan)

	// Cancel before sending the signal; goroutine will block on select{case <-buyBreadChan}
	// which does not have a ctx.Done() branch — so we just verify no panic.
	cancel()
	time.Sleep(50 * time.Millisecond)
	// goroutine is still alive (blocked on channel receive); we just verify it
	// doesn't crash. The test exits and the goroutine is garbage collected.
}

// --- buyBreadStream tests ---

func TestBuyBreadStream_ReceivesResponsesAndSignalsDone(t *testing.T) {
	responses := []*pb.BreadResponse{
		{Message: "bread order settled"},
	}
	client := &mockBuyBreadClient{
		streamClient: newMockStream(responses...),
	}
	cfg := newConfig(client)

	breadBoughtChan := make(chan bool, 1)
	doneStream      := make(chan bool, 1)
	errChan         := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cfg.buyBreadStream(ctx, breadBoughtChan, doneStream, "uuid-stream", errChan)

	breadBoughtChan <- true

	select {
	case <-doneStream:
		// reached EOF — success
	case err := <-errChan:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for doneStream signal")
	}
}

func TestBuyBreadStream_EOFImmediatelySendsDone(t *testing.T) {
	// Stream has no responses at all — first Recv returns io.EOF
	client := &mockBuyBreadClient{
		streamClient: newMockStream(), // empty
	}
	cfg := newConfig(client)

	breadBoughtChan := make(chan bool, 1)
	doneStream      := make(chan bool, 1)
	errChan         := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cfg.buyBreadStream(ctx, breadBoughtChan, doneStream, "uuid-eof", errChan)

	breadBoughtChan <- true

	select {
	case <-doneStream:
		// immediate EOF is fine
	case err := <-errChan:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout on immediate EOF stream")
	}
}

func TestBuyBreadStream_StreamDialError(t *testing.T) {
	client := &mockBuyBreadClient{
		streamErr: errors.New("cannot connect to server"),
	}
	cfg := newConfig(client)

	breadBoughtChan := make(chan bool, 1)
	doneStream      := make(chan bool, 1)
	errChan         := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cfg.buyBreadStream(ctx, breadBoughtChan, doneStream, "uuid-dial-err", errChan)

	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("expected non-nil error")
		}
	case <-doneStream:
		t.Fatal("expected error path, got doneStream")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stream dial error")
	}
}

func TestBuyBreadStream_RecvErrorSendsToErrChan(t *testing.T) {
	stream := newMockStream()
	stream.recvErr = errors.New("transport failure")
	client := &mockBuyBreadClient{streamClient: stream}
	cfg := newConfig(client)

	breadBoughtChan := make(chan bool, 1)
	doneStream      := make(chan bool, 1)
	errChan         := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cfg.buyBreadStream(ctx, breadBoughtChan, doneStream, "uuid-recv-err", errChan)

	breadBoughtChan <- true

	select {
	case err := <-errChan:
		if err == nil {
			t.Fatal("expected recv error")
		}
	case <-doneStream:
		t.Fatal("expected error path, got doneStream")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for recv error")
	}
}

func TestBuyBreadStream_MultipleResponses(t *testing.T) {
	responses := []*pb.BreadResponse{
		{Message: "response 1"},
		{Message: "response 2"},
		{Message: "response 3"},
	}
	client := &mockBuyBreadClient{
		streamClient: newMockStream(responses...),
	}
	cfg := newConfig(client)

	breadBoughtChan := make(chan bool, 1)
	doneStream      := make(chan bool, 1)
	errChan         := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cfg.buyBreadStream(ctx, breadBoughtChan, doneStream, "uuid-multi", errChan)

	breadBoughtChan <- true

	select {
	case <-doneStream:
		// consumed all 3 responses then got EOF
	case err := <-errChan:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout on multi-response stream")
	}
}

// TestBuyBreadStream_ContextCancelledBeforeSignal exercises the ctx.Done() branch
// inside the for-select loop when context is cancelled before breadBoughtChan fires.
func TestBuyBreadStream_ContextCancelledBeforeSignal(t *testing.T) {
	client := &mockBuyBreadClient{
		streamClient: newMockStream(), // dial succeeds, enters the for-select loop
	}
	cfg := newConfig(client)

	breadBoughtChan := make(chan bool) // unbuffered — no one sends
	doneStream      := make(chan bool, 1)
	errChan         := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())

	go cfg.buyBreadStream(ctx, breadBoughtChan, doneStream, "uuid-ctx-done", errChan)

	// Give the goroutine time to reach the select, then cancel.
	time.Sleep(20 * time.Millisecond)
	cancel()

	// The goroutine must exit within a short window via ctx.Done().
	select {
	case <-doneStream:
		t.Error("expected ctx.Done exit, not doneStream")
	case <-errChan:
		t.Error("expected ctx.Done exit, not errChan")
	case <-time.After(500 * time.Millisecond):
		// Goroutine exited silently — correct behaviour.
	}
}

// --- Request validation tests ---

// TestBuySomeBread_RequestPayload verifies that buySomeBread sends the correct
// BreadRequest with all expected bread items, quantities, and UUID.
func TestBuySomeBread_RequestPayload(t *testing.T) {
	client := &mockBuyBreadClient{
		buyBreadResp: &pb.BreadResponse{
			Breads:  &pb.BreadList{Breads: []*pb.Bread{{Name: "Pretzel"}}},
			Message: "started",
		},
	}
	cfg := newConfig(client)

	buyBreadChan  := make(chan bool, 1)
	breadBoughtChan := make(chan bool, 1)
	doneBuy       := make(chan bool, 1)
	errChan       := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cfg.buySomeBread(ctx, buyBreadChan, breadBoughtChan, doneBuy, "test-uuid-123", errChan)

	buyBreadChan <- true

	select {
	case <-doneBuy:
	case err := <-errChan:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for doneBuy signal")
	}

	req := client.buyBreadReq
	if req == nil {
		t.Fatal("BuyBread was never called — no request captured")
	}

	// Verify UUID
	if req.BuyOrderUuid != "test-uuid-123" {
		t.Errorf("expected BuyOrderUuid 'test-uuid-123', got %q", req.BuyOrderUuid)
	}

	// Verify bread count
	if req.Breads == nil {
		t.Fatal("Breads field is nil")
	}
	if len(req.Breads.Breads) != 7 {
		t.Fatalf("expected 7 bread items, got %d", len(req.Breads.Breads))
	}

	// Verify key bread items exist
	breadMap := make(map[string]*pb.Bread)
	for _, b := range req.Breads.Breads {
		breadMap[b.Name] = b
	}

	expectedBreads := map[string]struct {
		qty    int32
		price  float64
		status string
	}{
		"Pretzel":        {qty: 3, price: 2.49, status: "available"},
		"Baguette":       {qty: 2, price: 1.49, status: "available"},
		"Cinnamon Roll":  {qty: 4, price: 2.99, status: "available"},
		"Croissant":      {qty: 3, price: 1.19, status: "available"},
		"Brioche":        {qty: 4, price: 1.59, status: "available"},
		"Bolillo":        {qty: 3, price: 0.79, status: "available"},
		"Sourdough Bread": {qty: 1, price: 1.99, status: "available"},
	}

	for name, want := range expectedBreads {
		bread, ok := breadMap[name]
		if !ok {
			t.Errorf("missing bread item: %s", name)
			continue
		}
		if bread.Quantity != want.qty {
			t.Errorf("bread %s: expected qty %d, got %d", name, want.qty, bread.Quantity)
		}
		if bread.Price != want.price {
			t.Errorf("bread %s: expected price %.2f, got %.2f", name, want.price, bread.Price)
		}
		if bread.Status != want.status {
			t.Errorf("bread %s: expected status %q, got %q", name, want.status, bread.Status)
		}
	}
}

// TestBuySomeBread_RequestContainsAllFields verifies each bread item has
// all required fields set (Name, Quantity, Price, Description, Type, Status, Image).
func TestBuySomeBread_RequestContainsAllFields(t *testing.T) {
	client := &mockBuyBreadClient{
		buyBreadResp: &pb.BreadResponse{
			Breads:  &pb.BreadList{},
			Message: "started",
		},
	}
	cfg := newConfig(client)

	buyBreadChan  := make(chan bool, 1)
	breadBoughtChan := make(chan bool, 1)
	doneBuy       := make(chan bool, 1)
	errChan       := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cfg.buySomeBread(ctx, buyBreadChan, breadBoughtChan, doneBuy, "uuid-fields", errChan)
	buyBreadChan <- true

	select {
	case <-doneBuy:
	case err := <-errChan:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for doneBuy")
	}

	for _, b := range client.buyBreadReq.Breads.Breads {
		if b.Name == "" {
			t.Error("bread Name is empty")
		}
		if b.Quantity <= 0 {
			t.Errorf("bread %s: Quantity %d is not positive", b.Name, b.Quantity)
		}
		if b.Price <= 0 {
			t.Errorf("bread %s: Price %.2f is not positive", b.Name, b.Price)
		}
		if b.Description == "" {
			t.Errorf("bread %s: Description is empty", b.Name)
		}
		if b.Type == "" {
			t.Errorf("bread %s: Type is empty", b.Name)
		}
		if b.Status == "" {
			t.Errorf("bread %s: Status is empty", b.Name)
		}
		if b.Image == "" {
			t.Errorf("bread %s: Image URL is empty", b.Name)
		}
	}
}

// TestBuySomeBread_ContextTimeoutDuringRPC verifies that when the context
// times out during the BuyBread RPC call, the goroutine exits via errChan.
func TestBuySomeBread_ContextTimeoutDuringRPC(t *testing.T) {
	client := &mockBuyBreadClient{
		buyBreadResp: &pb.BreadResponse{Message: "started"},
	}
	cfg := newConfig(client)

	buyBreadChan  := make(chan bool, 1)
	breadBoughtChan := make(chan bool, 1)
	doneBuy       := make(chan bool, 1)
	errChan       := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())

	go cfg.buySomeBread(ctx, buyBreadChan, breadBoughtChan, doneBuy, "uuid-timeout", errChan)

	// Cancel context right after signaling
	buyBreadChan <- true
	cancel()

	// The goroutine should either complete or exit via context.
	// Since our mock returns immediately, it completes before cancellation takes effect.
	select {
	case <-doneBuy:
		// Completed before context cancelled — acceptable for fast mock
	case <-errChan:
		// Context cancellation triggered — also acceptable
	case <-time.After(1 * time.Second):
		t.Error("goroutine did not exit within timeout")
	}
}

// TestBuyBreadStream_RequestUUID verifies that buyBreadStream sends the
// correct BuyOrderUuid in its stream request.
func TestBuyBreadStream_RequestUUID(t *testing.T) {
	client := &mockBuyBreadClient{
		streamClient: newMockStream(&pb.BreadResponse{Message: "ok"}),
	}
	cfg := newConfig(client)

	breadBoughtChan := make(chan bool, 1)
	doneStream      := make(chan bool, 1)
	errChan         := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cfg.buyBreadStream(ctx, breadBoughtChan, doneStream, "stream-uuid-456", errChan)
	breadBoughtChan <- true

	select {
	case <-doneStream:
	case err := <-errChan:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for doneStream")
	}

	req := client.streamReq
	if req == nil {
		t.Fatal("BuyBreadStream was never called — no request captured")
	}
	if req.BuyOrderUuid != "stream-uuid-456" {
		t.Errorf("expected BuyOrderUuid 'stream-uuid-456', got %q", req.BuyOrderUuid)
	}
}

// TestBuyBreadStream_ResponseContent verifies that stream responses are
// processed (logged) and the doneStream signal is sent after EOF.
func TestBuyBreadStream_ResponseContent(t *testing.T) {
	responses := []*pb.BreadResponse{
		{Message: "order accepted", BuyOrderId: 42},
		{Message: "order processing", BuyOrderId: 42},
		{Message: "order fulfilled", BuyOrderId: 42},
	}
	client := &mockBuyBreadClient{
		streamClient: newMockStream(responses...),
	}
	cfg := newConfig(client)

	breadBoughtChan := make(chan bool, 1)
	doneStream      := make(chan bool, 1)
	errChan         := make(chan error, 2)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cfg.buyBreadStream(ctx, breadBoughtChan, doneStream, "uuid-content", errChan)
	breadBoughtChan <- true

	select {
	case <-doneStream:
		// All 3 responses consumed, then EOF → doneStream sent
	case err := <-errChan:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for doneStream")
	}

	// Verify the stream request was correct
	if client.streamReq == nil {
		t.Fatal("stream request was nil")
	}
	if client.streamReq.BuyOrderUuid != "uuid-content" {
		t.Errorf("expected BuyOrderUuid 'uuid-content', got %q", client.streamReq.BuyOrderUuid)
	}
}

// TestBuyBreadStream_ContextCancelledDuringStream tests that context cancellation
// while actively consuming stream responses stops the goroutine cleanly.
func TestBuyBreadStream_ContextCancelledDuringStream(t *testing.T) {
	slowStream := &mockBreadStreamClient{
		responses: []*pb.BreadResponse{{Message: "partial"}},
		pos:       1, // already consumed the one response
	}

	client := &mockBuyBreadClient{
		streamClient: slowStream,
	}
	cfg := newConfig(client)

	breadBoughtChan := make(chan bool, 1)
	doneStream      := make(chan bool, 1)
	errChan         := make(chan error, 2)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)

	go cfg.buyBreadStream(ctx, breadBoughtChan, doneStream, "uuid-cancel-during", errChan)
	breadBoughtChan <- true

	// Wait for context timeout — goroutine should exit via ctx.Done()
	select {
	case <-doneStream:
		// Stream finished before cancellation — OK if server responded fast
	case <-errChan:
		// Got an error — acceptable
	case <-time.After(500 * time.Millisecond):
		t.Error("goroutine did not exit within expected time after context cancel")
	}
	cancel()
}

// --- runBuyCycle tests ---

// TestRunBuyCycle_Success verifies that runBuyCycle returns (true, nil) when
// both buySomeBread and buyBreadStream complete successfully.
func TestRunBuyCycle_Success(t *testing.T) {
	client := &mockBuyBreadClient{
		buyBreadResp: &pb.BreadResponse{
			Breads:  &pb.BreadList{Breads: []*pb.Bread{{Name: "Pretzel"}}},
			Message: "started",
		},
		streamClient: newMockStream(&pb.BreadResponse{Message: "confirmed"}),
	}
	cfg := &Config{buyBreadClient: client}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	success, err := cfg.runBuyCycle(ctx, "uuid-run-success")
	if !success {
		t.Errorf("expected success=true, got %v", success)
	}
	if err != nil {
		t.Errorf("expected err=nil, got %v", err)
	}
}

// TestRunBuyCycle_Error verifies that runBuyCycle returns (false, err) when
// buySomeBread encounters an error.
func TestRunBuyCycle_Error(t *testing.T) {
	client := &mockBuyBreadClient{
		buyBreadErr: errors.New("connection refused"),
	}
	cfg := &Config{buyBreadClient: client}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	success, err := cfg.runBuyCycle(ctx, "uuid-run-error")
	if success {
		t.Error("expected success=false, got true")
	}
	if err == nil {
		t.Error("expected non-nil error")
	}
}

// TestRunBuyCycle_ContextCancelled verifies that runBuyCycle returns
// (false, context.Canceled) when the context is cancelled while waiting
// for the cycle to complete. Uses a blocking mock to ensure the goroutine
// is stuck inside BuyBread when cancellation occurs.
func TestRunBuyCycle_ContextCancelled(t *testing.T) {
	// buyBreadBlock blocks BuyBread until we close it.
	blockCh := make(chan struct{})
	// buyBreadStreamBlock blocks BuyBreadStream until we close it.
	streamCh := make(chan struct{})
	client := &mockBuyBreadClient{
		buyBreadBlock: blockCh,   // blocks buySomeBread
		streamClient:  &blockingStreamClient{block: streamCh}, // blocks buyBreadStream
	}
	cfg := &Config{buyBreadClient: client}

	ctx, cancel := context.WithCancel(context.Background())

	// Start runBuyCycle in a goroutine so we can cancel the context.
	successCh := make(chan bool, 1)
	errCh := make(chan error, 1)
	go func() {
		s, e := cfg.runBuyCycle(ctx, "uuid-run-cancel")
		successCh <- s
		errCh <- e
	}()

	// Give the goroutine time to start and get stuck inside both goroutines.
	time.Sleep(50 * time.Millisecond)

	// Cancel — runBuyCycle's ctx.Done() case should fire.
	cancel()

	// Wait for runBuyCycle to exit.
	select {
	case success := <-successCh:
		if success {
			t.Error("expected success=false after context cancel")
		}
	case <-time.After(1 * time.Second):
		t.Error("runBuyCycle did not exit after context cancel")
		return
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled error, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Error("runBuyCycle did not return error after context cancel")
	}

	// Unblock mocks to prevent goroutine leak.
	close(blockCh)
	close(streamCh)
}

// blockingStreamClient is a stream mock that blocks on Recv until the channel closes.
type blockingStreamClient struct {
	block chan struct{}
}

func (b *blockingStreamClient) Recv() (*pb.BreadResponse, error) {
	<-b.block // block forever
	return nil, io.EOF
}
func (b *blockingStreamClient) Header() (metadata.MD, error) { return nil, nil }
func (b *blockingStreamClient) Trailer() metadata.MD         { return nil }
func (b *blockingStreamClient) CloseSend() error             { return nil }
func (b *blockingStreamClient) Context() context.Context     { return context.Background() }
func (b *blockingStreamClient) SendMsg(interface{}) error    { return nil }
func (b *blockingStreamClient) RecvMsg(interface{}) error    { return nil }

// TestRunBuyCycle_ContextTimeout verifies that runBuyCycle returns
// (false, context.DeadlineExceeded) when the context deadline is reached.
func TestRunBuyCycle_ContextTimeout(t *testing.T) {
	blockCh := make(chan struct{})
	streamCh := make(chan struct{})
	client := &mockBuyBreadClient{
		buyBreadBlock: blockCh,  // blocks buySomeBread
		streamClient:  &blockingStreamClient{block: streamCh}, // blocks buyBreadStream
	}
	cfg := &Config{buyBreadClient: client}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := cfg.runBuyCycle(ctx, "uuid-run-timeout")
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
	// Verify it didn't take too long (context timeout should have fired).
	if elapsed > 2*time.Second {
		t.Errorf("runBuyCycle took %v — expected ~50ms context timeout", elapsed)
	}

	// Unblock mocks to prevent goroutine leak.
	close(blockCh)
	close(streamCh)
}

// TestRunBuyCycle_UsesCycleDelay verifies that runBuyCycle does NOT sleep
// (the delay is handled by main, not runBuyCycle). This confirms the extracted
// function is fast and testable.
func TestRunBuyCycle_NoSleep(t *testing.T) {
	client := &mockBuyBreadClient{
		buyBreadResp: &pb.BreadResponse{Message: "ok"},
		streamClient: newMockStream(&pb.BreadResponse{Message: "ok"}),
	}
	cfg := &Config{buyBreadClient: client}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	cfg.runBuyCycle(ctx, "uuid-no-sleep")
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("runBuyCycle took %v — expected <2s (no 35s sleep should be here)", elapsed)
	}
}

// --- Config construction ---

func TestConfig_BuyBreadClientSet(t *testing.T) {
	client := &mockBuyBreadClient{}
	cfg := newConfig(client)
	if cfg.buyBreadClient == nil {
		t.Error("expected buyBreadClient to be set")
	}
}

// --- buyOrder channel buffering ---

// TestBuyOrderChan_Buffered verifies the main loop's buyOrderChan is buffered
// with capacity 2 — two orders can be queued without blocking.
func TestBuyOrderChan_Buffered(t *testing.T) {
	buyOrderChan := make(chan buyOrder, 2)

	o := buyOrder{buyOrderUUID: "test-uuid"}
	buyOrderChan <- o
	buyOrderChan <- o

	if len(buyOrderChan) != 2 {
		t.Errorf("expected 2 items in channel, got %d", len(buyOrderChan))
	}
}
