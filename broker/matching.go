package main

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	rabbitmq "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

// dbTimeout is reused from the data package.
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
func maxBidPrice(o data.BuyOrder) float32 {
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

// processMatchingBatch takes a batch of pending orders and fulfills them
// according to priority: highest bid first, then earliest sequence number.
// Per-item fulfillment, partial fulfillment, skip, and reject logic are applied.
func (app *RabbitMQBakery) processMatchingBatch(orders []data.BuyOrder, pub publisher) {
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

	for idx := range orders {
		order := &orders[idx]
		app.fulfillOrder(order, pub)
	}
}

// fulfillOrder processes a single order within a matched batch.
// It atomically checks stock and deducts for each item, then updates status.
func (app *RabbitMQBakery) fulfillOrder(order *data.BuyOrder, pub publisher) {
	uuid := order.BuyOrderUUID
	log.WithField("order_uuid", uuid).Info("matching: processing order")

	// Build matched items from the order's bread list.
	items := make([]data.OrderItem, len(order.Breads))
	var totalQuantityRequested int
	var totalQuantityFulfilled int
	var hasRejected bool

	for i, bread := range order.Breads {
		item := data.OrderItem{
			BreadID:           bread.ID,
			QuantityRequested: bread.Quantity,
			BidPrice:          order.BidPrice,
		}

		if bread.Quantity <= 0 {
			item.Status = "skipped"
			items[i] = item
			continue
		}

		// Atomic stock check + deduction via FulfillOrderItem (SELECT FOR UPDATE).
		fulfilled, err := app.Repo.FulfillOrderItem(bread.ID, bread.Quantity)
		if err != nil {
			log.WithField("bread_id", bread.ID).Warnf("matching: insufficient stock for %s", bread.Name)
			if order.AllowPartial || order.SkipUnavailableItems {
				item.Status = "skipped"
			} else {
				item.Status = "rejected"
				hasRejected = true
			}
		} else {
			item.QuantityFulfilled = fulfilled
			item.Status = "fulfilled"
			totalQuantityFulfilled += fulfilled
		}
		totalQuantityRequested += bread.Quantity
		items[i] = item
	}

	order.MatchedItems = items

	// Determine order-level status.
	if hasRejected && !order.AllowPartial {
		order.Status = "rejected"
	} else if totalQuantityFulfilled == totalQuantityRequested && totalQuantityRequested > 0 {
		order.Status = "processed"
	} else if totalQuantityFulfilled > 0 {
		order.Status = "partially_processed"
	} else {
		order.Status = "failed"
	}

	// Update order status in the database.
	if err := app.Repo.UpdateOrderStatus(uuid, order.Status); err != nil {
		log.Errorf("matching: failed to update order %s status to %s: %v", uuid, order.Status, err)
	}

	// Publish per-item result.
	result := publishResult{
		Order:     *order,
		Items:     items,
		TotalCost: 0,
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		log.Errorf("matching: failed to marshal result for %s: %v", uuid, err)
		return
	}

	if err := pub.Publish("", "bread-bought", false, false, rabbitmq.Publishing{
		ContentType:  "text/json",
		Body:         resultJSON,
		DeliveryMode: rabbitmq.Persistent,
	}); err != nil {
		log.Errorf("matching: failed to publish result for %s: %v", uuid, err)
	}

	log.WithField("order_uuid", uuid).WithField("status", order.Status).Info("matching: order processed")
}

// publishResult is the per-item result published to the bread-bought queue.
type publishResult struct {
	Order     data.BuyOrder   `json:"order"`
	Items     []data.OrderItem `json:"items"`
	TotalCost float32         `json:"total_cost"`
}
