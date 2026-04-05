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

	// Clear in reverse dependency order
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
		"bread",
		"outbox",
	}

	for _, table := range tables {
		if _, err := h.DB.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", table)); err != nil {
			return fmt.Errorf("failed to clear table %s: %w", table, err)
		}
	}

	// Reset sequences
	sequences := []string{
		"customer_id_seq",
		"buy_id_seq",
		"bread_id_seq",
		"bread_maker_id_seq",
		"make_order_id_seq",
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

// ResetSequences resets all sequences to 1
func (h *DBHelper) ResetSequences() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sequences := []string{
		"customer_id_seq",
		"buy_id_seq",
		"bread_id_seq",
		"bread_maker_id_seq",
		"make_order_id_seq",
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
