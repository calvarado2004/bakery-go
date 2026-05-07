package main

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	rabbitmq "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

const dbTimeout = time.Second * 5

// matchBatchSize is the maximum number of orders to process in a single batch.
const matchBatchSize = 100

// matchBatchWindow is the maximum time to wait before processing a batch.
const matchBatchWindow = 500 * time.Millisecond

// orderBuffer holds incoming orders waiting to be matched.
type orderBuffer struct {
	mu     sync.Mutex
	orders []data.BuyOrder
}

func (b *orderBuffer) add(o data.BuyOrder) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.orders = append(b.orders, o)
}

func (b *orderBuffer) drain() []data.BuyOrder {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.orders) == 0 {
		return nil
	}
	out := b.orders
	b.orders = nil
	return out
}

func (b *orderBuffer) len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.orders)
}

// maxBidPrice returns the highest bid price in the order's items.
// Orders with no explicit bid price get 0 (lowest priority).
func maxBidPrice(o data.BuyOrder) float64 {
	if o.BidPrice > 0 {
		return o.BidPrice
	}
	for _, item := range o.MatchedItems {
		if item.BidPrice > o.BidPrice {
			o.BidPrice = item.BidPrice
		}
	}
	return o.BidPrice
}

// publisher is the subset of rabbitmq.Channel used by processMatchingBatch.
// Defined here so tests can stub out the network call.
type publisher interface {
	Publish(exchange, key string, mandatory, immediate bool, msg rabbitmq.Publishing) error
}

// processMatchingBatch takes a batch of pending orders and fulfills them
// according to priority: highest bid first, then earliest sequence number.
// All data operations go through the server's gRPC BrokerService.
func (svc *BrokerService) processMatchingBatch(orders []data.BuyOrder, pub publisher, bc brokerClienter) {
	if len(orders) == 0 {
		return
	}

	// Sort by priority: highest bid price first, then earliest sequence number.
	sort.Slice(orders, func(i, j int) bool {
		bidI := maxBidPrice(orders[i])
		bidJ := maxBidPrice(orders[j])
		if bidI != bidJ {
			return bidI > bidJ // higher bid wins
		}
		return orders[i].SequenceNumber < orders[j].SequenceNumber // tie-break by time
	})

	log.Infof("matchingBatch: processing %d orders (sorted by priority)", len(orders))

	// Process each order in the matching engine, collecting results.
	var batchResults []*pb.MatchingBatchResult

	for idx := range orders {
		order := &orders[idx]
		result := svc.fulfillOrder(order, pub, bc)
		if result != nil {
			batchResults = append(batchResults, result)
		}
	}

	// Report all matching results to the server in one gRPC call.
	// The server updates order statuses and writes to outbox atomically.
	if len(batchResults) > 0 {
		matchBatch := &pb.MatchingBatch{
			Results: batchResults,
		}

		confirmation, err := bc.ReportMatchingResults(matchBatch)
		if err != nil {
			log.Errorf("matching: failed to report matching results to server: %v", err)
		} else {
			log.Infof("matching: server confirmed %d orders processed", confirmation.OrdersProcessed)
		}
	}
}

// fulfillOrder processes a single order within a matched batch.
// It calls the server's gRPC ReserveInventory for each item (atomic stock check + deduction),
// then builds the matching result. Returns nil if the order was skipped (no items to fulfill).
func (svc *BrokerService) fulfillOrder(order *data.BuyOrder, pub publisher, bc brokerClienter) *pb.MatchingBatchResult {
	uuid := order.BuyOrderUUID
	log.WithField("order_uuid", uuid).Info("matching: processing order")

	var totalQuantityRequested int
	var totalQuantityFulfilled int
	var hasRejected bool

	var items []*pb.MatchingItemResult
	var totalCost float64

	for _, bread := range order.Breads {
		if bread.Quantity <= 0 {
			items = append(items, &pb.MatchingItemResult{
				BreadId:           int32(bread.ID),
				QuantityRequested: int32(bread.Quantity),
				Status:            "skipped",
			})
			continue
		}

		// Call server's gRPC ReserveInventory (atomic SELECT FOR UPDATE + deduct).
		resResult, err := bc.ReserveInventory(&pb.ReserveInventoryRequest{
			BreadId:           int32(bread.ID),
			QuantityRequested: int32(bread.Quantity),
			BuyOrderUuid:      uuid,
		})
		if err != nil || !resResult.Reserved {
			log.WithField("bread_id", bread.ID).Warnf("matching: insufficient stock for %s", bread.Name)
			status := "rejected"
			if order.AllowPartial || order.SkipUnavailableItems {
				status = "skipped"
			} else {
				hasRejected = true
			}
			items = append(items, &pb.MatchingItemResult{
				BreadId:           int32(bread.ID),
				QuantityRequested: int32(bread.Quantity),
				Status:            status,
			})
			continue
		}

		fulfilled := int(resResult.QuantityFulfilled)
		items = append(items, &pb.MatchingItemResult{
			BreadId:           int32(bread.ID),
			QuantityRequested: int32(bread.Quantity),
			QuantityFulfilled: resResult.QuantityFulfilled,
			Status:            "fulfilled",
		})
		totalQuantityFulfilled += fulfilled
		totalCost += float64(fulfilled) * bread.Price
	}

	// Calculate total requested quantity.
	totalQuantityRequested = 0
	for _, bread := range order.Breads {
		totalQuantityRequested += bread.Quantity
	}

	// Determine order-level status.
	var orderStatus string
	if hasRejected && !order.AllowPartial {
		orderStatus = "rejected"
	} else if totalQuantityFulfilled == totalQuantityRequested && totalQuantityRequested > 0 {
		orderStatus = "processed"
	} else if totalQuantityFulfilled > 0 {
		orderStatus = "partially_processed"
	} else {
		orderStatus = "failed"
	}

	log.WithField("order_uuid", uuid).WithField("status", orderStatus).Info("matching: order result built")

	result := &pb.MatchingBatchResult{
		BuyOrderUuid: uuid,
		OrderStatus:  orderStatus,
		Items:        items,
		TotalCost:    totalCost,
	}

	// Also publish per-item result to bread-bought for the settlement dispatcher.
	publishResult := publishResult{
		Order:     *order,
		Items:     matchingItemsToDataOrderItems(items),
		TotalCost: totalCost,
	}
	resultJSON, err := json.Marshal(publishResult)
	if err != nil {
		log.Errorf("matching: failed to marshal result for %s: %v", uuid, err)
	} else {
		if err := pub.Publish("", "bread-bought", false, false, rabbitmq.Publishing{
			ContentType:  "text/json",
			Body:         resultJSON,
			DeliveryMode: rabbitmq.Persistent,
		}); err != nil {
			log.Errorf("matching: failed to publish result for %s: %v", uuid, err)
		}
	}

	log.WithField("order_uuid", uuid).WithField("status", orderStatus).Info("matching: order processed")

	return result
}

// matchingItemsToDataOrderItems converts proto MatchingItemResult to data.OrderItem.
func matchingItemsToDataOrderItems(items []*pb.MatchingItemResult) []data.OrderItem {
	result := make([]data.OrderItem, len(items))
	for i, item := range items {
		result[i] = data.OrderItem{
			BreadID:           int(item.BreadId),
			QuantityRequested: int(item.QuantityRequested),
			QuantityFulfilled: int(item.QuantityFulfilled),
			Status:            item.Status,
		}
	}
	return result
}

// publishResult is the per-item result published to the bread-bought queue.
type publishResult struct {
	Order     data.BuyOrder    `json:"order"`
	Items     []data.OrderItem `json:"items"`
	TotalCost float64          `json:"total_cost"`
}
