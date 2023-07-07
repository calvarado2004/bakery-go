package data

type Repository interface {
	InsertCustomer(customer Customer) (int, error)
	InsertBread(bread Bread) (int, error)
	InsertBreadMaker(baker BreadMaker) (int, error)
	InsertBuyOrder(order BuyOrder) (int, error)
	InsertMakeOrder(order MakeOrder) (int, error)
	AdjustBreadQuantity(breadID int, quantityChange int) error
	AdjustBreadPrice(breadID int, newPrice float64) error
	PasswordMatches(plainText string, customer Customer) (bool, error)
}
