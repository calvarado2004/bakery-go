package main

// Integration tests for broker moved to server/broker_service tests
// The broker no longer has direct DB access — all operations go through gRPC.
// Integration testing is handled at the server level.

