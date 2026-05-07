-- ========================================================================
-- Bakery Service — Development Seed Data
-- ========================================================================
-- WARNING: This file contains default credentials for local development
-- only. NEVER run this in production or commit real passwords.
--
-- Default credentials:
--   Admin:    admin / admin123
--   Customer: john@doe.com / password123
--
-- To use in local development:
--   psql -U postgres -d bakery -f seed-dev.sql
-- ========================================================================

-- Insert default customer (password: password123 - bcrypt hash)
INSERT INTO public.customer (name, email, password, created_at, updated_at) VALUES (
    'John Doe',
    'john@doe.com',
    '$2a$10$lWlfcAs2n8hT4z9PV/90EehZ5J04JQjz9B1fFO.GDUuVjyE/OlIr2',
    now(),
    now()
    );

INSERT INTO public.bread_maker (name, email, created_at, updated_at) VALUES (
    'Jake Maker',
    'jake@maker.com',
    now(),
    now()
    );

-- Insert default admin user (username: admin, password: admin123 - bcrypt hash)
INSERT INTO public.admin_users (username, email, password, role, created_at, updated_at) VALUES (
    'admin',
    'admin@bakery.com',
    '$2a$10$PHZBNmARXoZUa4WAHRbYpePNJiYGQPUTkeKWdzq28E8it2BfypDyq',
    'admin',
    now(),
    now()
    );

-- Insert seed bread items for testing (required for integration and e2e tests)
INSERT INTO public.bread (name, price, quantity, description, type, status, image, created_at, updated_at) VALUES
    ('Sourdough', 6.99, 50, 'Classic sourdough bread', 'Bread', 'available', '/images/sourdough.png', now(), now()),
    ('Croissant', 3.49, 100, 'Buttery French croissant', 'Pastry', 'available', '/images/croissant.png', now(), now()),
    ('Baguette', 4.99, 75, 'Traditional French baguette', 'Bread', 'available', '/images/baguette.png', now(), now()),
    ('Chocolate Cake', 12.99, 20, 'Rich chocolate layer cake', 'Cake', 'available', '/images/chocolate_cake.png', now(), now()),
    ('Blueberry Muffin', 3.99, 60, 'Fresh blueberry muffin', 'Pastry', 'available', '/images/muffin.png', now(), now()),
    ('Rye Bread', 5.49, 40, 'Dense rye bread', 'Bread', 'available', '/images/rye.png', now(), now()),
    ('Bagel', 2.99, 80, 'Toasted sesame bagel', 'Bread', 'available', '/images/bagel.png', now(), now());
