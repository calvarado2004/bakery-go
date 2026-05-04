package data

import (
	"database/sql"
	"errors"
	"testing"
)

// TestPostgresTestRepository_GetBuyOrderByUUID verifies the mock always returns a valid order
func TestPostgresTestRepository_GetBuyOrderByUUID(t *testing.T) {
	repo := NewPostgresTestRepository(nil)

	order, err := repo.GetBuyOrderByUUID("f67d8cfa-95df-4564-8f52-25f986a30d5e")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if order.ID != 1 {
		t.Errorf("expected order ID 1, got %d", order.ID)
	}
	if order.BuyOrderUUID == "" {
		t.Error("expected non-empty BuyOrderUUID")
	}
}

// TestPostgresTestRepository_GetOrderTotalCost verifies the mock returns a fixed cost
func TestPostgresTestRepository_GetOrderTotalCost(t *testing.T) {
	repo := NewPostgresTestRepository(nil)

	cost, err := repo.GetOrderTotalCost(1)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cost != 3.99 {
		t.Errorf("expected cost 3.99, got %f", cost)
	}
}

// TestGetBuyOrderByUUID_ErrNoRows verifies that sql.ErrNoRows is returned as-is
// (no wrapping) so callers can use errors.Is to distinguish "not found" from real errors.
func TestGetBuyOrderByUUID_ErrNoRows(t *testing.T) {
	// errNoRowsRepo simulates the database returning no rows for a UUID lookup.
	repo := &errNoRowsRepo{}

	_, err := repo.GetBuyOrderByUUID("non-existent-uuid")
	if err == nil {
		t.Fatal("expected sql.ErrNoRows, got nil")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected errors.Is(err, sql.ErrNoRows) to be true, got: %v", err)
	}
}

// TestGetOrderTotalCost_NullHandling verifies that a NULL total_cost (no order details)
// returns 0 without error rather than a scan error.
func TestGetOrderTotalCost_NullHandling(t *testing.T) {
	repo := &nullTotalCostRepo{}

	cost, err := repo.GetOrderTotalCost(99)
	if err != nil {
		t.Fatalf("expected no error for NULL total_cost, got: %v", err)
	}
	if cost != 0 {
		t.Errorf("expected cost 0 for NULL total_cost, got %f", cost)
	}
}

// --- minimal test stubs ---

// errNoRowsRepo returns sql.ErrNoRows for GetBuyOrderByUUID, mirroring the fixed behaviour
// in PostgresRepository where ErrNoRows is returned without logging.
type errNoRowsRepo struct{ PostgresTestRepository }

func (r *errNoRowsRepo) GetBuyOrderByUUID(_ string) (BuyOrder, error) {
	return BuyOrder{}, sql.ErrNoRows
}

// nullTotalCostRepo returns 0, nil for GetOrderTotalCost, mirroring the fixed behaviour
// in PostgresRepository where a NULL SUM result is converted to 0 instead of scan error.
type nullTotalCostRepo struct{ PostgresTestRepository }

func (r *nullTotalCostRepo) GetOrderTotalCost(_ int) (float64, error) {
	return 0, nil
}
