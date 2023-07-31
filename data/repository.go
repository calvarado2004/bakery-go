package data

type Repository interface {
	InsertCustomer(customer Customer) (int, error)
	InsertBread(bread Bread) (int, error)
	InsertBreadMaker(baker BreadMaker) (int, error)
	InsertBuyOrder(order BuyOrder, breads []Bread) (int, error)
	InsertMakeOrder(order MakeOrder, breads []Bread) (int, error)
	AdjustBreadQuantity(breadID int, quantityChange int) error
	AdjustBreadPrice(breadID int, newPrice float32) error
	PasswordMatches(plainText string, customer Customer) (bool, error)
	GetAvailableBread() ([]Bread, error)
	GetBreadByID(breadID int) (bread Bread, err error)
	GetMakeOrderByID(orderID int) (order MakeOrder, err error)
	GetBuyOrderByID(orderID int) (order BuyOrder, err error)
	GetBuyOrderByUUID(uuid string) (order BuyOrder, err error)
	UpdateOrderStatus(buyOrderUUID string, status string) error
	GetOrderTotalCost(orderID int) (float32, error)
	DeleteOutboxMessage(id int) error
	InsertOutboxMessage(message OutboxMessage) error
	GetUnprocessedOutboxMessages() ([]OutboxMessage, error)
}
