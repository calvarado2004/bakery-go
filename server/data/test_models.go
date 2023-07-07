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
	return nil
}

func (u *PostgresTestRepository) AdjustBreadPrice(breadID int, newPrice float64) error {
	return nil
}
