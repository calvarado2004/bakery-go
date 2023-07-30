package data

import "database/sql"

type PostgresTestRepository struct {
	Conn *sql.DB
}

func NewPostgresTestRepository(db *sql.DB) *PostgresTestRepository {
	return &PostgresTestRepository{
		Conn: db,
	}
}

func (u *PostgresTestRepository) UpdateOrderStatus(buyOrderUUID string, status string) error {
	return nil
}

func (u *PostgresTestRepository) GetOrderTotalCost(orderID int) (float32, error) {

	return 3.99, nil
}

func (u *PostgresTestRepository) InsertCustomer(customer Customer) (int, error) {
	return 1, nil
}

func (u *PostgresTestRepository) InsertBread(bread Bread) (int, error) {
	return 1, nil
}

func (u *PostgresTestRepository) InsertBuyOrder(order BuyOrder, breads []Bread) (int, error) {
	return 1, nil
}

func (u *PostgresTestRepository) InsertBreadMaker(baker BreadMaker) (int, error) {
	return 1, nil
}

func (u *PostgresTestRepository) InsertMakeOrder(order MakeOrder, breads []Bread) (int, error) {
	return 1, nil
}

func (u *PostgresTestRepository) PasswordMatches(plainText string, customer Customer) (bool, error) {

	return true, nil
}

func (u *PostgresTestRepository) AdjustBreadQuantity(breadID int, quantityChange int) error {

	breadID = 1
	quantityChange = 1

	bread := Bread{
		ID:       breadID,
		Name:     "roll-test",
		Price:    1.0,
		Quantity: 1,
	}

	bread.Quantity += quantityChange

	return nil
}

func (u *PostgresTestRepository) AdjustBreadPrice(breadID int, newPrice float32) error {

	breadID = 1
	newPrice = 1.0

	bread := Bread{
		ID:       breadID,
		Name:     "roll-test",
		Price:    1.0,
		Quantity: 1,
	}

	bread.Price = newPrice

	return nil
}

func (u *PostgresTestRepository) GetAvailableBread() ([]Bread, error) {

	bread := Bread{
		ID:       1,
		Name:     "roll-test",
		Price:    1.0,
		Quantity: 1,
	}

	breads := []Bread{bread}
	return breads, nil
}

func (u *PostgresTestRepository) GetBreadByID(breadID int) (bread Bread, err error) {
	breadID = 1

	bread = Bread{
		ID:       breadID,
		Name:     "roll-test",
		Price:    1.0,
		Quantity: 1,
	}

	return bread, nil

}

func (u *PostgresTestRepository) GetMakeOrderByID(orderID int) (order MakeOrder, err error) {
	orderID = 1

	order = MakeOrder{
		ID:           orderID,
		BreadMakerID: 1,
	}

	return order, nil

}

func (u *PostgresTestRepository) GetBuyOrderByID(orderID int) (order BuyOrder, err error) {

	orderID = 1

	customer := Customer{
		ID:       1,
		Name:     "John Doe",
		Email:    "john@doe.com",
		Password: "password",
	}

	bread := Bread{
		ID:       1,
		Name:     "roll-test",
		Price:    1.0,
		Quantity: 1,
	}

	breads := []Bread{bread}

	order = BuyOrder{
		ID:       orderID,
		Customer: customer,
		Breads:   breads,
	}

	return order, nil
}

func (u *PostgresTestRepository) GetBuyOrderByUUID(orderUUID string) (order BuyOrder, err error) {

	orderUUID = "f67d8cfa-95df-4564-8f52-25f986a30d5e"

	customer := Customer{
		ID:       1,
		Name:     "John Doe",
		Email:    "john@doe.com",
		Password: "password",
	}

	bread := Bread{
		ID:       1,
		Name:     "roll-test",
		Price:    1.0,
		Quantity: 1,
	}

	breads := []Bread{bread}

	order = BuyOrder{
		ID:           1,
		Customer:     customer,
		Breads:       breads,
		BuyOrderUUID: orderUUID,
	}

	return order, nil
}
