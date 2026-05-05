package main

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// customerIDKey is the context key for passing customer ID between
// the gRPC interceptor and handlers.
type customerIDKeyType struct{}

var customerIDKey customerIDKeyType

// GetCustomerIDFromContext extracts the customer ID from the gRPC context.
// It reads the value set by the customerIDInterceptor from metadata.
// Returns 0 if no customer ID is present.
func GetCustomerIDFromContext(ctx context.Context) int {
	if v, ok := ctx.Value(customerIDKey).(string); ok {
		var id int
		if _, err := fmt.Sscanf(v, "%d", &id); err == nil {
			return id
		}
	}
	return 0
}

// customerIDInterceptor is a gRPC unary interceptor that extracts the
// customer_id from incoming metadata and injects it into the gRPC context.
//
// The frontend client attaches customer_id as gRPC metadata via
// metadata.Pairs("customer_id", "<id>") when a customer is authenticated.
// This interceptor converts that metadata into a context value so that
// handlers (e.g. BuyBread) can reliably identify the authenticated customer.
// (ARCHITECTURE_AUDIT §6.6)
func customerIDInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if ok {
		if ids, present := md["customer_id"]; present && len(ids) > 0 {
			customerID := ids[0]
			// Quick validation: must be digits only
			valid := true
			if len(customerID) == 0 {
				valid = false
			}
			for _, c := range customerID {
				if c < '0' || c > '9' {
					valid = false
					break
				}
			}
			if valid {
				ctx = context.WithValue(ctx, customerIDKey, customerID)
			}
		}
	}

	return handler(ctx, req)
}
