-- ============================================================
-- Seed data for local/dev testing
-- Run after schema.sql
-- ============================================================

USE ordersdb;

INSERT INTO customers (email, name) VALUES
    ('alice@example.com', 'Alice'),
    ('bob@example.com',   'Bob');

INSERT INTO products (sku, name, current_price, in_stock) VALUES
    ('SKU-001', 'Widget A', 9.99,  1),
    ('SKU-002', 'Widget B', 19.99, 1),
    ('SKU-003', 'Gadget C', 49.99, 0);

-- Alice: one checked-out cart (history) + one active cart
INSERT INTO carts (customer_id, status) VALUES (1, 'checked_out');
INSERT INTO carts (customer_id, status) VALUES (1, 'active');

-- Bob: one active cart
INSERT INTO carts (customer_id, status) VALUES (2, 'active');

-- Past order items (cart 1 — Alice's checked-out cart)
INSERT INTO cart_items (cart_id, product_id, quantity, unit_price) VALUES
    (1, 1, 2, 9.99),
    (1, 2, 1, 19.99);

-- Alice's current active cart (cart 2)
INSERT INTO cart_items (cart_id, product_id, quantity, unit_price) VALUES
    (2, 3, 1, 49.99);

-- Bob's active cart (cart 3)
INSERT INTO cart_items (cart_id, product_id, quantity, unit_price) VALUES
    (3, 1, 5, 9.99),
    (3, 2, 2, 19.99);
