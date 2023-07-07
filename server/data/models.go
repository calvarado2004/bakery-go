package data

import (
	"context"
	"database/sql"
	"errors"
	"golang.org/x/crypto/bcrypt"
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
	ID        int        `json:"primary_key"`
	Name      string     `json:"name"`
	Email     string     `json:"email"`
	Password  string     `json:"password"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	BuyOrders []BuyOrder `json:"buy_orders"`
}

type Bread struct {
	ID        int       `json:"primary_key"`
	Name      string    `json:"name"`
	Price     float64   `json:"price"`
	Quantity  int       `json:"quantity"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Image     string    `json:"image"`
}

type BuyOrder struct {
	ID         int      `json:"primary_key"`
	CustomerID int      `json:"customer_id"`
	Customer   Customer `json:"customer"`
	Breads     []Bread  `json:"breads"`
}

type OrdersProcessed struct {
	ID         int       `json:"primary_key"`
	CustomerID int       `json:"customer_id"`
	BuyOrderID int       `json:"buy_order_id"`
	Customer   Customer  `json:"customer"`
	BuyOrder   BuyOrder  `json:"buy_order"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type BreadMaker struct {
	ID         int         `json:"primary_key"`
	Name       string      `json:"name"`
	Email      string      `json:"email"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
	MakeOrders []MakeOrder `json:"make_orders"`
}

type MakeOrder struct {
	ID           int        `json:"primary_key"`
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
	stmt := `INSERT INTO bread (name, price, quantity, created_at, updated_at, image) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`

	err := db.QueryRowContext(ctx, stmt,
		bread.Name,
		bread.Price,
		bread.Quantity,
		time.Now(),
		time.Now(),
		bread.Image,
	).Scan(&newID)

	if err != nil {
		return 0, err
	}

	return newID, nil
}

func (u *PostgresRepository) InsertBuyOrder(order BuyOrder) (int, error) {

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

func (u *PostgresRepository) InsertMakeOrder(order MakeOrder) (int, error) {

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

	return newID, nil
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
