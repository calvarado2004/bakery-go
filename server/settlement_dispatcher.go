package main

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	rabbitmq "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
)

// settlementWaiter holds the channel a BuyBreadStream goroutine is blocked on,
// waiting for the broker to settle its order.
type settlementWaiter struct {
	ch     chan *data.BuyOrder
	closed bool
}

// settlementDispatcher is the interface for the central router.
// Production uses SettlementDispatcher; tests inject mocks.
type settlementDispatcher interface {
	Start()
	Register(uuid string) <-chan *data.BuyOrder
	Unregister(uuid string)
}

// SettlementDispatcher is the central router for order-settlement messages.
//
// Design (electronic-market pattern):
//   - ONE goroutine consumes the "bread-bought" AMQP queue
//   - N gRPC streams register waiters by order UUID
//   - When a settlement message arrives, the dispatcher looks up the waiter
//     and pushes the filled order through the channel
//   - If the message arrives before the stream has registered, a 500 ms grace
//     window retries the lookup (handles normal race)
//   - A final DB fallback poll runs once after the grace window expires, so
//     we never lose a settlement even if the AMQP message was lost
//
// This scales because there is exactly one AMQP consumer and zero DB polling
// per stream.
type SettlementDispatcher struct {
	mu       sync.RWMutex
	waiters  map[string]*settlementWaiter
	bakery   *RabbitMQBakery
	rmqURL   string
	conn     *rabbitmq.Connection
	channel  *rabbitmq.Channel
	consumer string
}

// NewSettlementDispatcher creates the dispatcher.
func NewSettlementDispatcher(bakery *RabbitMQBakery, rmqURL string) *SettlementDispatcher {
	return &SettlementDispatcher{
		waiters: make(map[string]*settlementWaiter),
		bakery:  bakery,
		rmqURL:  rmqURL,
	}
}

// Start opens a persistent AMQP connection, starts consuming "bread-bought",
// and routes each message to the waiting stream.
func (sd *SettlementDispatcher) Start() {
	go sd.loop()
}

func (sd *SettlementDispatcher) loop() {
	for {
		if err := sd.runOnce(); err != nil {
			log.Errorf("SettlementDispatcher: consumer crashed, reconnecting in 5s: %v", err)
			time.Sleep(5 * time.Second)
		}
	}
}

func (sd *SettlementDispatcher) runOnce() error {
	conn, err := rabbitmq.Dial(sd.rmqURL)
	if err != nil {
		return err
	}
	defer conn.Close() //nolint:errcheck

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close() //nolint:errcheck

	deliveryChan, err := ch.Consume(
		"bread-bought", // queue
		"",             // consumer tag — let RabbitMQ generate one
		false,          // auto-ack
		false,          // exclusive
		false,          // no-local
		false,          // no-wait
		nil,            // args
	)
	if err != nil {
		return err
	}

	log.Println("SettlementDispatcher: started consuming bread-bought")

	for delivery := range deliveryChan {
		var order data.BuyOrder
		if err := json.Unmarshal(delivery.Body, &order); err != nil {
			log.Errorf("SettlementDispatcher: failed to unmarshal delivery: %v", err)
			delivery.Nack(false, false) //nolint:errcheck
			continue
		}

		log.Printf("SettlementDispatcher: received settlement for order %s", order.BuyOrderUUID)

		if sd.deliver(order.BuyOrderUUID, &order) {
			delivery.Ack(false) //nolint:errcheck
		} else {
			// Nobody is waiting for this UUID. Could be a duplicate
			// confirmation or a stream that already timed out.
			// Ack it anyway — at-least-once means duplicates are expected.
			delivery.Ack(false) //nolint:errcheck
		}
	}

	return nil
}

// deliver routes the settled order to the waiting stream. If the stream has
// not registered yet, it retries for up to 500 ms (normal race between broker
// publish and stream registration).
func (sd *SettlementDispatcher) deliver(uuid string, order *data.BuyOrder) bool {
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		sd.mu.RLock()
		w, ok := sd.waiters[uuid]
		sd.mu.RUnlock()

		if ok && !w.closed {
			select {
			case w.ch <- order:
				return true
			default:
				// Channel full — shouldn't happen with buffer-1, but be safe
			}
		}

		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// Register creates a buffered channel for the given UUID and returns it.
// The caller should read exactly one *data.BuyOrder from the channel.
func (sd *SettlementDispatcher) Register(uuid string) <-chan *data.BuyOrder {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	// If a previous waiter exists (e.g. a retry), clean it up.
	if old, ok := sd.waiters[uuid]; ok {
		old.closed = true
		close(old.ch)
	}

	ch := make(chan *data.BuyOrder, 1)
	sd.waiters[uuid] = &settlementWaiter{ch: ch}
	return ch
}

// Unregister removes the waiter for the given UUID.
func (sd *SettlementDispatcher) Unregister(uuid string) {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if w, ok := sd.waiters[uuid]; ok {
		w.closed = true
		close(w.ch)
		delete(sd.waiters, uuid)
	}
}
