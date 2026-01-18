package data

type DashboardStats struct {
	TotalOrders     int
	TotalRevenue    float32
	TotalProducts   int
	TotalCustomers  int
	TotalBreadMakers int
	LowStockCount   int
}

type Repository interface {
	InsertCustomer(customer Customer) (int, error)
	InsertBread(bread Bread) (int, error)
	InsertBreadMaker(baker BreadMaker) (int, error)
	InsertBuyOrder(order BuyOrder, breads []Bread) (int, error)
	InsertMakeOrder(order MakeOrder, breads []Bread) (int, error)
	AdjustBreadQuantity(breadID int, quantityChange int) (bool, error)
	AdjustBreadPrice(breadID int, newPrice float32) error
	PasswordMatches(plainText string, customer Customer) (bool, error)
	GetAvailableBread() ([]Bread, error)
	GetBreadByID(breadID int) (bread Bread, err error)
	GetMakeOrderByID(orderID int) (order MakeOrder, err error)
	GetBuyOrderByID(orderID int) (order BuyOrder, err error)
	GetBuyOrderByUUID(uuid string) (order BuyOrder, err error)
	GetAllBuyOrders() (orders []BuyOrder, err error)
	UpdateOrderStatus(buyOrderUUID string, status string) error
	GetOrderTotalCost(orderID int) (float32, error)
	DeleteOutboxMessage(id int) error
	InsertOutboxMessage(message OutboxMessage) error
	GetUnprocessedOutboxMessages() ([]OutboxMessage, error)
	// Admin methods
	GetAllCustomers() ([]Customer, error)
	GetAllBreadMakers() ([]BreadMaker, error)
	GetDashboardStats() (*DashboardStats, error)
	UpdateBread(bread Bread) error
	DeleteBread(breadID int) error
	GetLowStockBread(threshold int) ([]Bread, error)
	GetCustomerOrders(customerID int) ([]BuyOrder, error)
	GetMakerOrders(makerID int) ([]MakeOrder, error)
	GetCustomerByID(customerID int) (Customer, error)
	GetBreadMakerByID(makerID int) (BreadMaker, error)
	GetAllMakeOrders() ([]MakeOrder, error)
}
