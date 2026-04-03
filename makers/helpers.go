package main

import (
	"encoding/json"

	"github.com/calvarado2004/bakery-go/data"
)

// processMakeBreadMessage unmarshals a RabbitMQ message body into a Bread and
// adjusts its quantity in the repository.  It is extracted from
// listenForMakeBread to allow unit testing without a live RabbitMQ connection.
func processMakeBreadMessage(repo data.Repository, body []byte) error {
	bread := &data.Bread{}
	if err := json.Unmarshal(body, bread); err != nil {
		return err
	}
	_, err := repo.AdjustBreadQuantity(bread.ID, bread.Quantity)
	return err
}
