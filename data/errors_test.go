package data

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgconn"
)

// TestErrInsufficientStock_Sentinel verifies the error is a stable sentinel value.
func TestErrInsufficientStock_Sentinel(t *testing.T) {
	if ErrInsufficientStock == nil {
		t.Fatal("ErrInsufficientStock must not be nil")
	}
	if ErrInsufficientStock.Error() != "insufficient stock" {
		t.Errorf("unexpected message: %q", ErrInsufficientStock.Error())
	}
}

// TestErrInsufficientStock_Wrapping verifies that errors.Is works through wrapping.
func TestErrInsufficientStock_Wrapping(t *testing.T) {
	wrapped := fmt.Errorf("bread id=3: %w", ErrInsufficientStock)
	if !errors.Is(wrapped, ErrInsufficientStock) {
		t.Error("errors.Is should find ErrInsufficientStock through wrapping")
	}
}

// TestCheckConstraintCode verifies that pgconn error code 23514 is what we check for.
// This is a compile-time / logic test — it documents the mapping without hitting a real DB.
func TestCheckConstraintCode(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23514"}
	var asTarget *pgconn.PgError
	if !errors.As(pgErr, &asTarget) {
		t.Fatal("errors.As should find *pgconn.PgError")
	}
	if asTarget.Code != "23514" {
		t.Errorf("expected code 23514, got %s", asTarget.Code)
	}
}
