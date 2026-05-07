package testutils

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DBHelper provides helper methods for database operations in tests
type DBHelper struct {
	DB *sql.DB
}

// NewDBHelper creates a new database helper
func NewDBHelper(db *sql.DB) *DBHelper {
	return &DBHelper{DB: db}
}

// ClearAllTables clears all tables in the database
func (h *DBHelper) ClearAllTables() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Clear in reverse dependency order.
	// pending_make_orders has a FK to bread, so it must be cleared BEFORE bread.
	tables := []string{
		"invoice_items",
		"invoices",
		"admin_users",
		"orders_processed",
		"order_details",
		"buy_order",
		"customer",
		"make_order_details",
		"make_order",
		"bread_maker",
		"pending_make_orders",
		"bread",
		"outbox",
	}

	for _, table := range tables {
		if _, err := h.DB.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", table)); err != nil {
			return fmt.Errorf("failed to clear table %s: %w", table, err)
		}
	}

	// Reset sequences (including pending_make_order_id_seq)
	sequences := []string{
		"customer_id_seq",
		"buy_id_seq",
		"bread_id_seq",
		"bread_maker_id_seq",
		"make_order_id_seq",
		"pending_make_order_id_seq",
		"orders_processed_id_seq",
		"invoice_id_seq",
		"admin_user_id_seq",
		"invoice_item_id_seq",
	}

	for _, seq := range sequences {
		if _, err := h.DB.ExecContext(ctx, fmt.Sprintf("ALTER SEQUENCE %s RESTART WITH 1", seq)); err != nil {
			return fmt.Errorf("failed to reset sequence %s: %w", seq, err)
		}
	}

	return nil
}

// ResetSequences resets all sequences to 1 (including pending_make_order_id_seq).
func (h *DBHelper) ResetSequences() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sequences := []string{
		"customer_id_seq",
		"buy_id_seq",
		"bread_id_seq",
		"bread_maker_id_seq",
		"make_order_id_seq",
		"pending_make_order_id_seq",
		"orders_processed_id_seq",
		"invoice_id_seq",
		"admin_user_id_seq",
		"invoice_item_id_seq",
	}

	for _, seq := range sequences {
		if _, err := h.DB.ExecContext(ctx, fmt.Sprintf("ALTER SEQUENCE %s RESTART WITH 1", seq)); err != nil {
			return fmt.Errorf("failed to reset sequence %s: %w", seq, err)
		}
	}

	return nil
}

// GetTableCount returns the number of rows in a table
func (h *DBHelper) GetTableCount(tableName string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var count int
	if err := h.DB.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// SeedTestBread inserts default bread items into the database.
// This is needed after ClearAllTables() since integration tests often
// require bread data for inventory-related operations.
func (h *DBHelper) SeedTestBread() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	breads := []struct {
		Name        string
		Price       float64
		Quantity    int
		Description string
		Type        string
		Status      string
		Image       string
	}{
		{"Sourdough", 6.99, 50, "Classic sourdough bread", "Bread", "available", "/images/sourdough.png"},
		{"Croissant", 3.49, 100, "Buttery French croissant", "Pastry", "available", "/images/croissant.png"},
		{"Baguette", 4.99, 75, "Traditional French baguette", "Bread", "available", "/images/baguette.png"},
		{"Chocolate Cake", 12.99, 20, "Rich chocolate layer cake", "Cake", "available", "/images/chocolate_cake.png"},
		{"Blueberry Muffin", 3.99, 60, "Fresh blueberry muffin", "Pastry", "available", "/images/muffin.png"},
		{"Rye Bread", 5.49, 40, "Dense rye bread", "Bread", "available", "/images/rye.png"},
		{"Bagel", 2.99, 80, "Toasted sesame bagel", "Bread", "available", "/images/bagel.png"},
	}

	stmt := `INSERT INTO bread (name, price, quantity, description, type, status, image)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`

	for _, b := range breads {
		if _, err := h.DB.ExecContext(ctx, stmt, b.Name, b.Price, b.Quantity, b.Description, b.Type, b.Status, b.Image); err != nil {
			return fmt.Errorf("seed bread %s: %w", b.Name, err)
		}
	}

	return nil
}
