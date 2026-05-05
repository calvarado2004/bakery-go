package main

import "github.com/calvarado2004/bakery-go/data"

// canFulfillOrder checks whether every bread in the order has sufficient
// quantity in the available-bread list.  Returns true only if every ordered
// bread has enough stock; a bread that does not appear in available at all is
// treated as unavailable (returns false).
//
// NOTE: This function is kept for reference/testing only. The matching engine
// now uses the server's gRPC ReserveInventory (atomic SELECT FOR UPDATE)
// instead of this in-memory check.
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
