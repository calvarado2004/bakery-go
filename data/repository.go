package data

import "time"

type DashboardStats struct {
	TotalOrders      int
	TotalRevenue     float32
	TotalProducts    int
	TotalCustomers   int
	TotalBreadMakers int
	LowStockCount    int
}

type AdminUser struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Password  string    `json:"-"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Invoice struct {
	ID            int           `json:"id"`
	BuyOrderID    int           `json:"buy_order_id"`
	CustomerID    int           `json:"customer_id"`
	InvoiceNumber string        `json:"invoice_number"`
	Subtotal      float32       `json:"subtotal"`
	Tax           float32       `json:"tax"`
	Total         float32       `json:"total"`
	Status        string        `json:"status"`
	CreatedAt     time.Time     `json:"created_at"`
	DueDate       time.Time     `json:"due_date"`
	PaidAt        *time.Time    `json:"paid_at,omitempty"`
	Items         []InvoiceItem `json:"items"`
}

type InvoiceItem struct {
	ID        int     `json:"id"`
	InvoiceID int     `json:"invoice_id"`
	BreadID   int     `json:"bread_id"`
	BreadName string  `json:"bread_name"`
	Quantity  int     `json:"quantity"`
	UnitPrice float32 `json:"unit_price"`
	Total     float32 `json:"total"`
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
	// Auth methods
	GetAdminUserByUsername(username string) (AdminUser, error)
	GetAdminUserByID(id int) (AdminUser, error)
	InsertAdminUser(user AdminUser) (int, error)
	GetCustomerByEmail(email string) (Customer, error)
	// Invoice methods
	InsertInvoice(invoice Invoice) (int, error)
	GetInvoiceByID(id int) (Invoice, error)
	GetInvoicesByCustomerID(customerID int) ([]Invoice, error)
	GetAllInvoices() ([]Invoice, error)
	GetInvoiceByOrderID(orderID int) (Invoice, error)
}
