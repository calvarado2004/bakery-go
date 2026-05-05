package main

import (
	"encoding/json"
	"github.com/calvarado2004/bakery-go/data"
	rabbitmq "github.com/rabbitmq/amqp091-go"
	log "github.com/sirupsen/logrus"
	"time"
)

// init is called before the application starts and declares queues owned by the server.
// In the external/internal boundary design (ARCHITECTURE_AUDIT §10.6), each service
// declares the queues it owns:
//   - Server owns: bread-made (for maker confirmations)
//   - Broker owns: buy-bread-order, bread-bought
//   - Makers own: make-bread-order
func (rabbit *RabbitMQBakery) init() {

	connection, err := rabbitmq.Dial(rabbit.rabbitmqURL)
	if err != nil {
		log.Errorf("Failed to connect to RabbitMQ: %v", err)
		return
	}
	defer func(conn *rabbitmq.Connection) {
		err := conn.Close()
		if err != nil {
			log.Errorf("Failed to close connection: %v", err)
		}
	}(connection)

	channel, err := connection.Channel()
	if err != nil {
		log.Errorf("Failed to open a channel: %v", err)
		return
	}
	defer func(ch *rabbitmq.Channel) {
		err := ch.Close()
		if err != nil {
			log.Errorf("Failed to close channel: %v", err)
		}
	}(channel)

	// Declare the RabbitMQ bread-made queue (consumed by maker confirmations).
	// Server is the sole owner; external makers publish to this queue.
	_, err = channel.QueueDeclare(
		"bread-made", // name
		true,         // durable
		false,        // delete when unused
		false,        // exclusive
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		log.Fatalf("Failed to declare bread-made queue: %v", err)
	}

	log.Println("Server declared queue: bread-made")

}

// checkBread checks if there is enough bread left in the bakery, if not, it orders more
func (rabbit *RabbitMQBakery) checkBread() error {
	breads, err := rabbit.Repo.GetAvailableBread()
	if err != nil {
		return err
	}

	if len(breads) == 0 {
		rabbit.initializeBakery()
		return nil
	}

	// Write auto-replenishment requests to pending_make_orders table
	// AND publish to make-bread-order RabbitMQ queue so makers can process them.
	for _, bread := range breads {
		if bread.Quantity <= 10 {
			log.Printf("Low stock: %s (%d remaining), creating replenishment request for 50", bread.Name, bread.Quantity)

			_, err := rabbit.Repo.InsertPendingMakeOrder(data.PendingMakeOrder{
				BreadID:           bread.ID,
				RequestedQuantity: 50,
				Status:            "pending",
				Source:            "auto",
			})
			if err != nil {
				log.Errorf("Failed to create pending make order for bread %d: %v", bread.ID, err)
				continue
			}

			// Publish make-bread-order to RabbitMQ so makers can consume and bake
			makeMsg := map[string]interface{}{
				"id":          bread.ID,
				"name":        bread.Name,
				"quantity":    50,
				"description": bread.Description,
				"type":        bread.Type,
				"price":       bread.Price,
				"image":       bread.Image,
				"status":      "pending",
			}
			msgData, err := json.Marshal(makeMsg)
			if err != nil {
				log.Errorf("Failed to marshal make-bread message for %s: %v", bread.Name, err)
				continue
			}

			conn, err := rabbitmq.Dial(rabbit.rabbitmqURL)
			if err != nil {
				log.Errorf("Failed to connect to RabbitMQ for make-bread publish: %v", err)
				continue
			}
			ch, err := conn.Channel()
			if err != nil {
				log.Errorf("Failed to open channel for make-bread publish: %v", err)
				conn.Close()
				continue
			}

			err = ch.Publish(
				"",
				"make-bread-order",
				false,
				false,
				rabbitmq.Publishing{
					ContentType: "application/json",
					Body:        msgData,
				})
			if err != nil {
				log.Errorf("Failed to publish make-bread-order for %s: %v", bread.Name, err)
			} else {
				log.Printf("Published make-bread-order for %s to RabbitMQ", bread.Name)
			}
			ch.Close()
			conn.Close()
		} else {
			log.Printf("Enough bread of %s left, there are available %d", bread.Name, bread.Quantity)
		}
	}

	return nil

}

// initializeBakery creates the initial breads in the database
func (rabbit *RabbitMQBakery) initializeBakery() {

	breads := []data.Bread{
		{
			Name:        "Cinnamon Roll",
			Quantity:    1,
			Price:       2.99,
			Description: "Cinnamon Roll, a classic bakery bread with cinnamon and sugar",
			Type:        "Sweet Bread",
			Status:      "available",
			Image:       "https://cdn.pixabay.com/photo/2019/12/25/17/55/cinnamon-roll-4719023_1280.jpg",
		},
		{
			Name:        "Sourdough Bread",
			Quantity:    1,
			Price:       1.99,
			Description: "Sourdough Bread, a classic bakery bread with a sour taste",
			Type:        "Sour Bread",
			Status:      "available",
			Image:       "https://cdn.pixabay.com/photo/2020/11/28/12/25/bread-5784572_1280.jpg",
		},
		{
			Name:        "Baguette",
			Quantity:    1,
			Price:       1.49,
			Description: "Baguette, a classic bakery bread with a long shape",
			Type:        "French Bread",
			Status:      "available",
			Image:       "https://cdn.pixabay.com/photo/2017/06/23/23/57/bread-2436370_1280.jpg",
		},
		{
			Name:        "Pretzel",
			Quantity:    1,
			Price:       2.49,
			Description: "Pretzel, a classic bakery bread with a salty taste",
			Type:        "Salty Bread",
			Status:      "available",
			Image:       "https://cdn.pixabay.com/photo/2017/09/05/17/18/pretzel-2718477_1280.jpg",
		},
		{
			Name:        "Bolillo",
			Quantity:    1,
			Price:       0.79,
			Description: "Bolillo, a classic bakery bread with a soft texture",
			Type:        "Soft Bread",
			Status:      "available",
			Image:       "https://cdn.pixabay.com/photo/2019/02/07/21/19/bobbin-lace-3982200_1280.jpg",
		}, {
			Name:        "Croissant",
			Quantity:    1,
			Price:       1.19,
			Description: "Croissant, a classic bakery bread with a buttery taste",
			Type:        "Buttery Bread",
			Status:      "available",
			Image:       "https://cdn.pixabay.com/photo/2012/02/29/12/17/bread-18987_1280.jpg",
		},
		{
			Name:        "Brioche",
			Quantity:    1,
			Price:       1.59,
			Description: "Brioche, a classic bakery bread with a sweet taste",
			Type:        "Sweet Bread",
			Status:      "available",
			Image:       "https://cdn.pixabay.com/photo/2021/01/16/21/05/brioche-5923399_1280.jpg",
		},
	}

	for _, bread := range breads {
		breadID, err := rabbit.Repo.InsertBread(bread)
		if err != nil {
			return
		}
		log.Printf("Bread ID %d created", breadID)
	}

	breadMaker := data.BreadMaker{
		ID:        2,
		Name:      "Another Bread Maker",
		Email:     "another_bread@maker.com",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	breadMakerID, err := rabbit.Repo.InsertBreadMaker(breadMaker)
	if err != nil {
		return

	}

	log.Printf("Bread Maker ID %d created", breadMakerID)

}

// breadMadeMessage is received from external makers confirming they've baked bread.
type breadMadeMessage struct {
	BreadID  int `json:"breadId"`
	Quantity int `json:"quantity"`
}

// listenForBreadMade listens for confirmation messages from external makers
// and updates the database inventory. In the external-makers design, makers
// never access the database directly — they only communicate via RabbitMQ.
// The server consumes bread-made confirmations and applies the inventory change.
func (rabbit *RabbitMQBakery) listenForBreadMade() {
	reconnectDelay := 10 * time.Second

	for {
		connection, err := rabbitmq.Dial(rabbit.rabbitmqURL)
		if err != nil {
			log.Errorf("Failed to connect to RabbitMQ for bread-made listener: %v", err)
			time.Sleep(reconnectDelay)
			continue
		}

		channel, err := connection.Channel()
		if err != nil {
			log.Errorf("Failed to open channel for bread-made listener: %v", err)
			connection.Close()
			time.Sleep(reconnectDelay)
			continue
		}

		// Set QoS for fair dispatch
		if err := channel.Qos(5, 0, false); err != nil {
			log.Errorf("Failed to set QoS for bread-made listener: %v", err)
			channel.Close()
			connection.Close()
			time.Sleep(reconnectDelay)
			continue
		}

		messages, err := channel.Consume(
			"bread-made", // queue
			"",           // consumer
			false,        // auto-ack
			false,        // exclusive
			false,        // no-local
			false,        // no-wait
			nil,          // args
		)
		if err != nil {
			log.Errorf("Failed to consume bread-made queue: %v", err)
			channel.Close()
			connection.Close()
			time.Sleep(reconnectDelay)
			continue
		}

		for d := range messages {
			var msg breadMadeMessage
			if err := json.Unmarshal(d.Body, &msg); err != nil {
				log.Errorf("Failed to unmarshal bread-made message: %v", err)
				d.Nack(false, false) // discard bad messages
				continue
			}

			log.Printf("Maker confirmed bread %d (ID=%d, qty=%d) — adjusting inventory", msg.BreadID, msg.BreadID, msg.Quantity)

			// Adjust inventory atomically on the database
			if _, err := rabbit.Repo.AdjustBreadQuantity(msg.BreadID, msg.Quantity); err != nil {
				log.Errorf("Failed to adjust bread quantity for ID %d: %v", msg.BreadID, err)
				d.Nack(false, true) // requeue for retry
				continue
			}

			d.Ack(false)
			log.Printf("Inventory updated: bread %d +%d units", msg.BreadID, msg.Quantity)
		}

		// Consumer channel closed — reconnect
		log.Println("bread-made listener: connection lost, reconnecting in", reconnectDelay)
		channel.Close()
		connection.Close()
		time.Sleep(reconnectDelay)
	}
}

