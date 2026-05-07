package main

import (
	"encoding/json"
	"testing"
)

// --- MakersService.ProcessMakeBreadMessage unit tests ---

// TestProcessMakeBreadMessage_ValidMessage verifies valid JSON unmarshals
// and returns a correct breadMadeMessage confirmation.
func TestProcessMakeBreadMessage_ValidMessage(t *testing.T) {
	bread := makeBreadMessage{
		ID:          3,
		Name:        "Baguette",
		Quantity:    50,
		Description: "A test baguette",
		Type:        "French Bread",
		Price:       5.99,
		Status:      "available",
	}
	body, err := json.Marshal(bread)
	if err != nil {
		t.Fatalf("failed to marshal bread message: %v", err)
	}

	svc := NewMakersService(nil)
	result, err := svc.ProcessMakeBreadMessage(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BreadID != 3 {
		t.Errorf("expected BreadID=3, got %d", result.BreadID)
	}
	if result.Quantity != 50 {
		t.Errorf("expected Quantity=50, got %d", result.Quantity)
	}
}

func TestProcessMakeBreadMessage_InvalidJSON(t *testing.T) {
	svc := NewMakersService(nil)
	_, err := svc.ProcessMakeBreadMessage([]byte("not json {{{"))
	if err == nil {
		t.Fatal("expected JSON unmarshal error, got nil")
	}
}

func TestProcessMakeBreadMessage_EmptyBytes(t *testing.T) {
	svc := NewMakersService(nil)
	_, err := svc.ProcessMakeBreadMessage([]byte{})
	// Empty bytes cause JSON unmarshal error
	if err == nil {
		t.Fatal("expected error for empty bytes, got nil")
	}
}

func TestProcessMakeBreadMessage_EmptyJSON(t *testing.T) {
	svc := NewMakersService(nil)
	result, err := svc.ProcessMakeBreadMessage([]byte("{}"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BreadID != 0 {
		t.Errorf("expected BreadID=0, got %d", result.BreadID)
	}
}

func TestProcessMakeBreadMessage_NilBody(t *testing.T) {
	svc := NewMakersService(nil)
	_, err := svc.ProcessMakeBreadMessage(nil)
	if err == nil {
		t.Fatal("expected error on nil body, got nil")
	}
}

func TestProcessMakeBreadMessage_ZeroQuantity(t *testing.T) {
	svc := NewMakersService(nil)
	bread := makeBreadMessage{ID: 5, Name: "Bolillo", Quantity: 0}
	body, _ := json.Marshal(bread)
	result, err := svc.ProcessMakeBreadMessage(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BreadID != 5 {
		t.Errorf("expected BreadID=5, got %d", result.BreadID)
	}
	if result.Quantity != 0 {
		t.Errorf("expected Quantity=0, got %d", result.Quantity)
	}
}

func TestProcessMakeBreadMessage_LargeQuantity(t *testing.T) {
	svc := NewMakersService(nil)
	bread := makeBreadMessage{ID: 2, Name: "Sourdough", Quantity: 1000}
	body, _ := json.Marshal(bread)
	result, err := svc.ProcessMakeBreadMessage(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Quantity != 1000 {
		t.Errorf("expected Quantity=1000, got %d", result.Quantity)
	}
}

func TestProcessMakeBreadMessage_AllBreadTypes(t *testing.T) {
	svc := NewMakersService(nil)
	breads := []makeBreadMessage{
		{ID: 1, Name: "Cinnamon Roll", Quantity: 50, Type: "Sweet Bread"},
		{ID: 2, Name: "Sourdough Bread", Quantity: 50, Type: "Sour Bread"},
		{ID: 3, Name: "Baguette", Quantity: 50, Type: "French Bread"},
		{ID: 4, Name: "Pretzel", Quantity: 50, Type: "Salty Bread"},
		{ID: 5, Name: "Bolillo", Quantity: 50, Type: "Soft Bread"},
		{ID: 6, Name: "Croissant", Quantity: 50, Type: "Buttery Bread"},
		{ID: 7, Name: "Brioche", Quantity: 50, Type: "Sweet Bread"},
	}

	for _, bread := range breads {
		body, err := json.Marshal(bread)
		if err != nil {
			t.Fatalf("bread %q: marshal error: %v", bread.Name, err)
		}

		result, err := svc.ProcessMakeBreadMessage(body)
		if err != nil {
			t.Fatalf("bread %q: unexpected error: %v", bread.Name, err)
		}
		if result.BreadID != bread.ID {
			t.Errorf("bread %q: expected BreadID=%d, got %d", bread.Name, bread.ID, result.BreadID)
		}
		if result.Quantity != bread.Quantity {
			t.Errorf("bread %q: expected Quantity=%d, got %d", bread.Name, bread.Quantity, result.Quantity)
		}
		t.Logf("bread %q: processed successfully", bread.Name)
	}
}

func TestProcessMakeBreadMessage_MalformedID(t *testing.T) {
	svc := NewMakersService(nil)
	body := []byte(`{"id": "not-a-number", "name": "Test"}`)
	_, err := svc.ProcessMakeBreadMessage(body)
	if err == nil {
		t.Fatal("expected unmarshal error for non-numeric ID, got nil")
	}
}

func TestProcessMakeBreadMessage_TruncatedJSON(t *testing.T) {
	svc := NewMakersService(nil)
	body := []byte(`{"id": 1, "name": "Test"`)
	_, err := svc.ProcessMakeBreadMessage(body)
	if err == nil {
		t.Fatal("expected error on truncated JSON, got nil")
	}
}

func TestProcessMakeBreadMessage_WrongType(t *testing.T) {
	svc := NewMakersService(nil)
	body := []byte(`42`)
	_, err := svc.ProcessMakeBreadMessage(body)
	if err == nil {
		t.Fatal("expected error on non-object JSON, got nil")
	}
}

func TestProcessMakeBreadMessage_LongString(t *testing.T) {
	svc := NewMakersService(nil)
	bread := makeBreadMessage{
		ID:          1,
		Name:        "VeryLongBreadName" + string(make([]byte, 10000)),
		Quantity:    50,
		Description: "Very long description",
		Type:        "Test",
	}
	body, err := json.Marshal(bread)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	result, err := svc.ProcessMakeBreadMessage(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BreadID != 1 {
		t.Errorf("expected BreadID=1, got %d", result.BreadID)
	}
}

// --- makeBreadMessage struct tests ---

func TestMakeBreadMessage_JSONRoundTrip(t *testing.T) {
	original := makeBreadMessage{
		ID:          7,
		Name:        "Brioche",
		Quantity:    25,
		Description: "Sweet French bread",
		Type:        "Sweet Bread",
		Price:       3.49,
		Status:      "available",
		Image:       "https://example.com/brioche.jpg",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded makeBreadMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID: expected %d, got %d", original.ID, decoded.ID)
	}
	if decoded.Name != original.Name {
		t.Errorf("Name: expected %q, got %q", original.Name, decoded.Name)
	}
	if decoded.Quantity != original.Quantity {
		t.Errorf("Quantity: expected %d, got %d", original.Quantity, decoded.Quantity)
	}
	if decoded.Price != original.Price {
		t.Errorf("Price: expected %f, got %f", original.Price, decoded.Price)
	}
	if decoded.Type != original.Type {
		t.Errorf("Type: expected %q, got %q", original.Type, decoded.Type)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status: expected %q, got %q", original.Status, decoded.Status)
	}
}

// --- breadMadeMessage struct tests ---

func TestBreadMadeMessage_JSONRoundTrip(t *testing.T) {
	original := breadMadeMessage{
		BreadID:  99,
		Quantity: 200,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded breadMadeMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.BreadID != original.BreadID {
		t.Errorf("BreadID: expected %d, got %d", original.BreadID, decoded.BreadID)
	}
	if decoded.Quantity != original.Quantity {
		t.Errorf("Quantity: expected %d, got %d", original.Quantity, decoded.Quantity)
	}
}
