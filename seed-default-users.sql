-- seed-default-users.sql
-- Default users for the Bakery Service platform.
-- Run this against the bakery database: psql -d bakery -f seed-default-users.sql

-- Default customers (required for unauthenticated BuyBread fallback to customer_id=1)
INSERT INTO customer (id, name, email, password, created_at) VALUES
(1, 'John Doe', 'john@doe.com', 'password123', NOW());

-- Admin user
INSERT INTO customer (id, name, email, password, created_at) VALUES
(2, 'Admin User', 'admin@bakery.com', 'admin123', NOW());

-- Bread Maker user
INSERT INTO customer (id, name, email, password, created_at) VALUES
(3, 'Bread Maker', 'maker@bakery.com', 'maker123', NOW());

-- Reset sequence to avoid future conflicts
SELECT setval('customer_id_seq', (SELECT COALESCE(MAX(id), 1) FROM customer));
