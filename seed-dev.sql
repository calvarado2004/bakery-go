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
