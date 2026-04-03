package main

import "github.com/calvarado2004/bakery-go/data"

// canFulfillOrder checks whether every bread in the order has sufficient
// quantity in the available-bread list.  Returns true only if every ordered
// bread has enough stock; a bread that does not appear in available at all is
// treated as unavailable (returns false).
func canFulfillOrder(order data.BuyOrder, available []data.Bread) bool {
	for _, ordered := range order.Breads {
		found := false
		for _, stock := range available {
			if stock.Name == ordered.Name {
				found = true
				if stock.Quantity < ordered.Quantity {
					return false
				}
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// processOrderItems deducts each ordered bread from the repository and returns
// the first error encountered.  The caller is responsible for rolling back or
// marking the order as failed when an error is returned.
func processOrderItems(repo data.Repository, order data.BuyOrder) error {
	for _, bread := range order.Breads {
		if _, err := repo.AdjustBreadQuantity(bread.ID, -bread.Quantity); err != nil {
			return err
		}
	}
	return nil
}
