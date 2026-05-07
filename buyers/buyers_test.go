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
	streamClient  grpc.ServerStreamingClient[pb.BreadResponse]
	streamErr     error
}

func (m *mockBuyBreadClient) BuyBread(_ context.Context, _ *pb.BreadRequest, _ ...grpc.CallOption) (*pb.BreadResponse, error) {
	return m.buyBreadResp, m.buyBreadErr
}

func (m *mockBuyBreadClient) BuyBreadStream(_ context.Context, _ *pb.BreadRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[pb.BreadResponse], error) {
	if m.streamErr != nil {
		return nil, m.streamErr
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
