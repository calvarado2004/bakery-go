package main

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

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
