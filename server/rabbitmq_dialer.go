package main

import (
	"os"

	rabbitmq "github.com/rabbitmq/amqp091-go"
)

// RabbitMQDialer abstracts RabbitMQ connection creation.
// Production uses realRabbitMQDialer (calls amqp.Dial).
// Tests inject a dialer that returns mock connections.
type RabbitMQDialer interface {
	Dial() (*rabbitmq.Connection, error)
}

// realRabbitMQDialer is the production implementation.
type realRabbitMQDialer struct{}

func (realRabbitMQDialer) Dial() (*rabbitmq.Connection, error) {
	return rabbitmq.Dial(os.Getenv("RABBITMQ_SERVICE_ADDR"))
}
