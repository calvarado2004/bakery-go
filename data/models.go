package data

import (
	"context"
	"database/sql"
	"errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
	"time"
)

const dbTimeout = time.Second * 5

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
	ID           int      `json:"id"`
	CustomerID   int      `json:"customer_id"`
	BuyOrderUUID string   `json:"buy_order_uuid"`
	Customer     Customer `json:"customer"`
	Breads       []Bread  `json:"breads"`
	Status       string   `json:"status"`
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
	ID            int        `json:"id"`
	BreadMakerID  int        `json:"bread_maker_id"`
	MakeOrderUUID string     `json:"make_order_uuid"`
	BreadMaker    BreadMaker `json:"bread_maker"`
	Breads        []Bread    `json:"breads"`
}

type OutboxMessage struct {
	ID        int       `json:"id"`
	Payload   []byte    `json:"payload"`
	Sent      bool      `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// UpdateOrderStatus updates the status of the order with the given Buy Order UUID
func (u *PostgresRepository) UpdateOrderStatus(buyOrderUUID string, status string) error {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `UPDATE buy_order SET status = $1 WHERE buy_order_uuid = $2`
	_, err := db.ExecContext(ctx, stmt, status, buyOrderUUID)

	if err != nil {
		log.Errorf("Error updating buy order status: %v", err)
	}

	return err
}

// GetOrderTotalCost retrieves the total cost of a given order id
func (u *PostgresRepository) GetOrderTotalCost(orderID int) (float32, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `SELECT sum(od.price * od.quantity) AS total_cost FROM buy_order bo, order_details od  WHERE bo.id = od.buy_order_id AND bo.id = $1;`

	var total float32
	err := db.QueryRowContext(ctx, stmt, orderID).Scan(&total)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// There were no rows, but otherwise no error occurred
			log.Errorf("No rows returned for order ID %d", orderID)
			return 0, nil
		}
		log.Errorf("Error getting order price: %v", err)
		return 0, err
	}

	return total, nil
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
		log.Errorf("Error inserting customer: %v", err)
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
		log.Errorf("Error inserting bread: %v", err)
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
		log.Errorf("Error inserting bread maker: %v", err)
		return 0, err
	}

	return newID, nil
}

// InsertBuyOrder inserts a new buy order into the database, along with the order details, and returns the new ID
func (u *PostgresRepository) InsertBuyOrder(order BuyOrder, breads []Bread) (int, error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	var countBread bool
	var newID int
	stmt := `INSERT INTO buy_order (customer_id, buy_order_uuid) VALUES ($1, $2) RETURNING id`

	err := db.QueryRowContext(ctx, stmt,
		order.CustomerID, order.BuyOrderUUID,
	).Scan(&newID)

	if err != nil {
		log.Errorf("Error inserting buy order: %v", err)
		return 0, err
	}

	for _, bread := range breads {
		// Start a transaction
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			log.Errorf("Error starting a transaction: %v", err)
			return 0, err
		}

		stmt = `INSERT INTO order_details (buy_order_id, bread_id, quantity, price) VALUES ($1, $2, $3, $4)`

		_, err = tx.ExecContext(ctx, stmt, newID, bread.ID, bread.Quantity, bread.Price)

		if err != nil {
			log.Errorf("Error inserting order details: %v", err)
			// Rollback the transaction for this bread
			err := tx.Rollback()
			if err != nil {
				log.Error("Error rolling back transaction: %v", err)
			}
			continue // This will skip to the next bread in the loop
		}

		countBread, err = u.AdjustBreadQuantity(bread.ID, -bread.Quantity)
		if err != nil || !countBread {
			if err != nil {
				log.Errorf("Error adjusting bread quantity: %v", err)
			}

			// Rollback the transaction for this bread
			err := tx.Rollback()
			if err != nil {
				log.Error("Error rolling back transaction: %v", err)
			}
			continue // This will skip to the next bread in the loop
		}

		// If everything went well, commit the transaction for this bread
		err = tx.Commit()
		if err != nil {
			log.Errorf("Error committing transaction: %v", err)
			return 0, err
		}
	}
	return newID, nil
}

func (u *PostgresRepository) InsertMakeOrder(order MakeOrder, breads []Bread) (int, error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	var newID int
	stmt := `INSERT INTO make_order (bread_maker_id, make_order_uuid) VALUES ($1, $2) RETURNING id`

	err := db.QueryRowContext(ctx, stmt,
		order.BreadMakerID,
		order.MakeOrderUUID,
	).Scan(&newID)

	if err != nil {
		log.Errorf("Error inserting make order: %v", err)
		return 0, err
	}

	for _, bread := range breads {

		stmt = `INSERT INTO make_order_details (make_order_id, bread_id, quantity) VALUES ($1, $2, $3)`

		_, err := db.ExecContext(ctx, stmt, newID, bread.ID, bread.Quantity)

		if err != nil {
			log.Errorf("Error inserting make order details: %v", err)
			return 0, err
		}

		_, err = u.AdjustBreadQuantity(bread.ID, bread.Quantity)
		if err != nil {
			log.Errorf("Error adjusting bread quantity: %v", err)
			return 0, err
		}
	}

	return newID, nil
}

// AdjustBreadQuantity adjusts the quantity of a bread by the given amount, and returns an error if the quantity goes below 0 after 3 attempts
func (u *PostgresRepository) AdjustBreadQuantity(breadID int, quantityChange int) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	var countBread bool

	countBread = true

	// Fetch the current quantity of the bread
	stmt := `SELECT quantity FROM bread WHERE id = $1`
	row := db.QueryRowContext(ctx, stmt, breadID)

	var currentQuantity int
	err := row.Scan(&currentQuantity)
	if err != nil {
		log.Errorf("Error fetching bread quantity: %v", err)
		countBread = false
		return countBread, err
	}

	// Calculate the new quantity after the adjustment
	newQuantity := currentQuantity + quantityChange

	log.Println("This is the newQuantity attempted", newQuantity)

	if newQuantity < 0 {
		log.Warningf("New intended bread quantity cannot be adjusted below 0, setting to 0")
		newQuantity = 0
		countBread = false
	}

	if newQuantity > 100 {
		log.Warningf("New intended bread quantity cannot be adjusted to be greater than 100, setting to 100")
		newQuantity = 100
	}

	if currentQuantity > 10 {
		log.Warningf("There are enough breads in stock, setting to the current quantity")
		newQuantity = currentQuantity
	}

	// Update the bread quantity
	stmt = `UPDATE bread SET quantity = $1 WHERE id = $2`
	_, err = db.ExecContext(ctx, stmt, newQuantity, breadID)
	if err != nil {
		log.Errorf("Error updating bread quantity: %v", err)
		countBread = false
		return countBread, err
	}

	return countBread, nil
}

func (u *PostgresRepository) AdjustBreadPrice(breadID int, newPrice float32) error {
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `UPDATE bread SET price = $1 WHERE id = $2`

	_, err := db.ExecContext(ctx, stmt, newPrice, breadID)
	if err != nil {
		log.Errorf("Error updating bread price: %v", err)
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
			log.Errorf("Error comparing password: %v", err)
			return false, err
		}
	}

	return true, nil
}

// GetAvailableBread returns all breads that have a quantity greater or equal than 0
func (u *PostgresRepository) GetAvailableBread() ([]Bread, error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `SELECT * FROM bread WHERE quantity >= 0`

	rows, err := db.QueryContext(ctx, stmt)
	if err != nil {
		log.Errorf("Error fetching bread: %v", err)
		return nil, err
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Errorf("Error closing rows while fetching bread: %v", err)
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
			log.Errorf("Error scanning bread: %v", err)
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
	if err != nil {
		log.Errorf("Error scanning bread by ID: %v", err)
		return bread, err
	}

	return bread, err
}

func (u *PostgresRepository) GetMakeOrderByID(orderID int) (order MakeOrder, err error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `SELECT * FROM make_order WHERE id = $1`

	err = db.QueryRowContext(ctx, stmt, orderID).Scan(
		&order.ID,
		&order.BreadMakerID,
		&order.MakeOrderUUID,
	)

	if err != nil {
		log.Errorf("Error scanning make order by ID: %v", err)
		return order, err
	}

	stmt = `SELECT bread_id, quantity FROM make_order_details WHERE make_order_id = $1`

	rows, err := db.QueryContext(ctx, stmt, orderID)
	if err != nil {
		log.Errorf("Error querying make order details: %v", err)
		return order, err
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Errorf("Error closing rows while fetching make order details: %v", err)
		}
	}(rows)

	var breads []Bread

	for rows.Next() {
		var breadID, quantity int
		err := rows.Scan(&breadID, &quantity)
		if err != nil {
			log.Errorf("Error scanning make order details: %v", err)
			return order, err
		}

		bread, err := u.GetBreadByID(breadID)
		if err != nil {
			log.Errorf("Error fetching bread by ID: %v", err)
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
		&order.BuyOrderUUID,
	)

	if err != nil {
		log.Errorf("Error scanning buy order by ID: %v", err)
		return order, err
	}

	stmt = `SELECT bread_id, quantity FROM order_details WHERE buy_order_id = $1`

	rows, err := db.QueryContext(ctx, stmt, orderID)
	if err != nil {
		log.Errorf("Error querying order details: %v", err)
		return order, err
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Errorf("Error closing rows while fetching order details: %v", err)
		}
	}(rows)

	var breads []Bread

	for rows.Next() {
		var breadID, quantity int
		err := rows.Scan(&breadID, &quantity)
		if err != nil {
			log.Errorf("Error scanning order details: %v", err)
			return order, err
		}

		bread, err := u.GetBreadByID(breadID)
		if err != nil {
			log.Errorf("Error fetching bread by ID: %v", err)
			return order, err
		}

		bread.Quantity = quantity
		breads = append(breads, bread)
	}

	order.Breads = breads

	return order, nil
}

func (u *PostgresRepository) GetBuyOrderByUUID(orderUUID string) (order BuyOrder, err error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `SELECT * FROM buy_order WHERE buy_order_uuid = $1`

	err = db.QueryRowContext(ctx, stmt, orderUUID).Scan(
		&order.ID,
		&order.CustomerID,
		&order.BuyOrderUUID,
		&order.Status,
	)

	if err != nil {
		log.Errorf("Error scanning buy order by UUID: %v", err)
		return order, err
	}

	stmt = `SELECT bread_id, quantity FROM order_details WHERE buy_order_id = $1`

	rows, err := db.QueryContext(ctx, stmt, order.ID)
	if err != nil {
		log.Errorf("Error querying order details: %v", err)
		return order, err
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Errorf("Error closing rows while fetching order details: %v", err)
		}
	}(rows)

	var breads []Bread

	for rows.Next() {
		var breadID, quantity int
		err := rows.Scan(&breadID, &quantity)
		if err != nil {
			log.Errorf("Error scanning order details: %v", err)
			return order, err
		}

		bread, err := u.GetBreadByID(breadID)
		if err != nil {
			log.Errorf("Error fetching bread by ID: %v", err)
			return order, err
		}

		bread.Quantity = quantity
		breads = append(breads, bread)
	}

	order.Breads = breads

	return order, nil

}

// InsertOutboxMessage inserts a message into the outbox table for later processing
func (u *PostgresRepository) InsertOutboxMessage(message OutboxMessage) error {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `INSERT INTO outbox (id, payload, sent, created_at) VALUES ($1, $2, $3, $4)`
	_, err := db.ExecContext(ctx, stmt, message.ID, message.Payload, message.Sent, message.CreatedAt)
	if err != nil {
		log.Errorf("Error inserting outbox message: %v", err)
		return err
	}

	return nil
}

// GetUnprocessedOutboxMessages returns all unprocessed outbox messages from the database
func (u *PostgresRepository) GetUnprocessedOutboxMessages() ([]OutboxMessage, error) {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `SELECT id, payload, sent, created_at FROM outbox WHERE sent = false`

	rows, err := u.Conn.QueryContext(ctx, stmt)
	if err != nil {
		log.Errorf("Error querying unprocessed outbox messages: %v", err)
		return nil, err
	}
	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Errorf("Error closing rows while fetching unprocessed outbox messages: %v", err)
		}
	}(rows)

	var messages []OutboxMessage
	for rows.Next() {
		var msg OutboxMessage
		if err := rows.Scan(&msg.ID, &msg.Payload, &msg.Sent, &msg.CreatedAt); err != nil {
			log.Errorf("Error scanning outbox message: %v", err)
			return nil, err
		}
		messages = append(messages, msg)
	}
	if err := rows.Err(); err != nil {
		log.Errorf("Error fetching rows: %v", err)
		return nil, err
	}

	return messages, nil
}

// DeleteOutboxMessage removes an outbox message from the database (as is no longer needed)
func (u *PostgresRepository) DeleteOutboxMessage(id int) error {

	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	stmt := `DELETE FROM outbox WHERE id = $1`

	_, err := u.Conn.ExecContext(ctx, stmt, id)
	if err != nil {
		log.Errorf("Error deleting outbox message: %v", err)
	}

	return nil
}
