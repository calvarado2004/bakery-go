package data

import "errors"

// ErrInsufficientStock is returned when a buy order cannot be fulfilled because
// one or more bread items do not have enough stock in the database.
// It is a sentinel value — callers should use errors.Is to check for it.
var ErrInsufficientStock = errors.New("insufficient stock")
