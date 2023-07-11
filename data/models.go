package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"log"
	"time"
)

const dbTimeout = time.Second * 3

var db *sql.DB

type PostgresRepository struct {
	Conn *sql.DB
}

func NewPostgresRepository(pool *sql.DB) *PostgresRepository {
	db = pool
	return &PostgresRepository{
		Conn: pool,
	}
}

type Customer struct {
	ID        int        `json:"id"`
	Name      string     `json:"name"`
	Email     string     `json:"email"`
	Password  string     `json:"password"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	BuyOrders []BuyOrder `json:"buy_orders"`
}

type Bread struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Price       float32   `json:"price"`
	Quantity    int       `json:"quantity"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Image       string    `json:"image"`
	Description string    `json:"description"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
}

type BuyOrder struct {
	ID         int      `json:"id"`
	CustomerID int      `json:"customer_id"`
	Customer   Customer `json:"customer"`
	Breads     []Bread  `json:"breads"`
}

type OrdersProcessed struct {
	ID         int       `json:"id"`
	CustomerID int       `json:"customer_id"`
	BuyOrderID int       `json:"buy_order_id"`
	Customer   Customer  `json:"customer"`
	BuyOrder   BuyOrder  `json:"buy_order"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type BreadMaker struct {
	ID         int         `json:"id"`
	Name       string      `json:"name"`
	Email      string      `json:"email"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
	MakeOrders []MakeOrder `json:"make_orders"`
}

type MakeOrder struct {
	ID           int        `json:"id"`
	BreadMakerID int        `json:"bread_maker_id"`
	BreadMaker   BreadMaker `json:"bread_maker"`
	Breads       []Bread    `json:"breads"`
}

func (u *PostgresRepository) InsertCustomer(customer Customer) (int, error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(customer.Password), 12)
	if err != nil {
		return 0, err
	}

	var newID int
	stmt := `insert into customer (email, name, password, created_at, updated_at) values ($1, $2, $3, $4, $5) returning id`

	err = db.QueryRowContext(ctx, stmt,
		customer.Email,
		customer.Name,
		hashedPassword,
		time.Now(),
		time.Now(),
	).Scan(&newID)

	if err != nil {
		return 0, err
	}

	return newID, nil
}

func (u *PostgresRepository) InsertBread(bread Bread) (int, error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	var newID int
	stmt := `INSERT INTO bread (name, price, quantity, created_at, updated_at, image, description, type, status) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`

	err := db.QueryRowContext(ctx, stmt,
		bread.Name,
		bread.Price,
		bread.Quantity,
		time.Now(),
		time.Now(),
		bread.Image,
		bread.Description,
		bread.Type,
		bread.Status,
	).Scan(&newID)

	if err != nil {
		return 0, err
	}

	return newID, nil
}

func (u *PostgresRepository) InsertBreadMaker(baker BreadMaker) (int, error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	var newID int
	stmt := `INSERT INTO bread_maker (name, email, created_at, updated_at) VALUES ($1, $2, $3, $4) RETURNING id`

	err := db.QueryRowContext(ctx, stmt,
		baker.Name,
		baker.Email,
		time.Now(),
		time.Now(),
	).Scan(&newID)

	if err != nil {
		return 0, err
	}

	return newID, nil
}

func (u *PostgresRepository) InsertBuyOrder(order BuyOrder, breads []Bread) (int, error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	var newID int
	stmt := `INSERT INTO buy_order (customer_id) VALUES ($1) RETURNING id`

	err := db.QueryRowContext(ctx, stmt,
		order.CustomerID,
	).Scan(&newID)

	if err != nil {
		return 0, err
	}

	for _, bread := range breads {
		stmt = `INSERT INTO order_details (buy_order_id, bread_id, quantity) VALUES ($1, $2, $3)`

		_, err := db.ExecContext(ctx, stmt, newID, bread.ID, bread.Quantity)

		if err != nil {
			return 0, err
		}

		err = u.AdjustBreadQuantity(bread.ID, -bread.Quantity)
		if err != nil {
			return 0, err
		}
	}

	return newID, nil
}

func (u *PostgresRepository) InsertMakeOrder(order MakeOrder, breads []Bread) (int, error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	var newID int
	stmt := `INSERT INTO make_order (bread_maker_id) VALUES ($1) RETURNING id`

	err := db.QueryRowContext(ctx, stmt,
		order.BreadMakerID,
	).Scan(&newID)

	if err != nil {
		return 0, err
	}

	for _, bread := range breads {

		stmt = `INSERT INTO make_order_details (make_order_id, bread_id, quantity) VALUES ($1, $2, $3)`

		_, err := db.ExecContext(ctx, stmt, newID, bread.ID, bread.Quantity)

		if err != nil {
			return 0, err
		}

		err = u.AdjustBreadQuantity(bread.ID, bread.Quantity)
		if err != nil {
			return 0, err
		}
	}

	return newID, nil
}

func (u *PostgresRepository) AdjustBreadQuantity(breadID int, quantityChange int) error {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	// Fetch the current quantity of the bread
	stmt := `SELECT quantity FROM bread WHERE id = $1`
	row := db.QueryRowContext(ctx, stmt, breadID)

	var currentQuantity int
	err := row.Scan(&currentQuantity)
	if err != nil {
		return err
	}

	// Calculate the new quantity after the adjustment
	newQuantity := currentQuantity + quantityChange

	// Check if the new quantity is within the allowed range
	if newQuantity < 0 || newQuantity > 50 {
		return fmt.Errorf("bread quantity cannot be adjusted outside the range of 0 to 50")
	}

	// Update the bread quantity
	stmt = `UPDATE bread SET quantity = CAST($1 AS integer) WHERE id = $2`
	_, err = db.ExecContext(ctx, stmt, newQuantity, breadID)
	if err != nil {
		return err
	}

	return nil
}

func (u *PostgresRepository) AdjustBreadPrice(breadID int, newPrice float32) error {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `UPDATE bread SET price = $1 WHERE id = $2`

	_, err := db.ExecContext(ctx, stmt, newPrice, breadID)
	if err != nil {
		return err
	}

	return nil
}

func (u *PostgresRepository) PasswordMatches(plainText string, customer Customer) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(customer.Password), []byte(plainText))
	if err != nil {
		switch {
		case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword):
			// invalid password
			return false, nil
		default:
			return false, err
		}
	}

	return true, nil
}

func (u *PostgresRepository) GetAvailableBread() ([]Bread, error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `SELECT * FROM bread WHERE quantity > 0`

	rows, err := db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, err
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Println(err)
		}
	}(rows)

	var breads []Bread

	for rows.Next() {
		var bread Bread
		err := rows.Scan(
			&bread.ID,
			&bread.Name,
			&bread.Price,
			&bread.Quantity,
			&bread.Description,
			&bread.Type,
			&bread.Status,
			&bread.CreatedAt,
			&bread.UpdatedAt,
			&bread.Image,
		)
		if err != nil {
			return nil, err
		}

		breads = append(breads, bread)
	}

	return breads, nil
}

func (u *PostgresRepository) GetBreadByID(breadID int) (bread Bread, err error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `SELECT * FROM bread WHERE id = $1`

	err = db.QueryRowContext(ctx, stmt, breadID).Scan(
		&bread.ID,
		&bread.Name,
		&bread.Price,
		&bread.Quantity,
		&bread.Description,
		&bread.Type,
		&bread.Status,
		&bread.CreatedAt,
		&bread.UpdatedAt,
		&bread.Image,
	)

	return bread, err
}

func (u *PostgresRepository) GetMakeOrderByID(orderID int) (order MakeOrder, err error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `SELECT * FROM make_order WHERE id = $1`

	err = db.QueryRowContext(ctx, stmt, orderID).Scan(
		&order.ID,
		&order.BreadMakerID,
	)

	if err != nil {
		return order, err
	}

	stmt = `SELECT bread_id, quantity FROM make_order_details WHERE make_order_id = $1`

	rows, err := db.QueryContext(ctx, stmt, orderID)
	if err != nil {
		return order, err
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Println(err)
		}
	}(rows)

	var breads []Bread

	for rows.Next() {
		var breadID, quantity int
		err := rows.Scan(&breadID, &quantity)
		if err != nil {
			return order, err
		}

		bread, err := u.GetBreadByID(breadID)
		if err != nil {
			return order, err
		}

		bread.Quantity = quantity
		breads = append(breads, bread)
	}

	order.Breads = breads

	return order, nil
}

func (u *PostgresRepository) GetBuyOrderByID(orderID int) (order BuyOrder, err error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `SELECT * FROM buy_order WHERE id = $1`

	err = db.QueryRowContext(ctx, stmt, orderID).Scan(
		&order.ID,
		&order.CustomerID,
	)

	if err != nil {
		return order, err
	}

	stmt = `SELECT bread_id, quantity FROM order_details WHERE buy_order_id = $1`

	rows, err := db.QueryContext(ctx, stmt, orderID)
	if err != nil {
		return order, err
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Println(err)
		}
	}(rows)

	var breads []Bread

	for rows.Next() {
		var breadID, quantity int
		err := rows.Scan(&breadID, &quantity)
		if err != nil {
			return order, err
		}

		bread, err := u.GetBreadByID(breadID)
		if err != nil {
			return order, err
		}

		bread.Quantity = quantity
		breads = append(breads, bread)
	}

	order.Breads = breads

	return order, nil
}
