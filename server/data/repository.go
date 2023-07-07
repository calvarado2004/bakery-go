package data

type Repository interface {
	InsertCustomer(customer Customer) (int, error)
	InsertBread(bread Bread) (int, error)
	InsertBreadMaker(baker BreadMaker) (int, error)
	InsertBuyOrder(order BuyOrder, breads []Bread) (int, error)
	InsertMakeOrder(order MakeOrder, breads []Bread) (int, error)
	AdjustBreadQuantity(breadID int, quantityChange int) error
	AdjustBreadPrice(breadID int, newPrice float64) error
	PasswordMatches(plainText string, customer Customer) (bool, error)
}
