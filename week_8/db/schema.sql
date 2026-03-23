-- ============================================================
-- Shopping Cart Schema — MySQL 8.0
-- Database: ordersdb
-- ============================================================

USE ordersdb;

-- ============================================================
-- TABLE 1: customers
-- ============================================================
CREATE TABLE IF NOT EXISTS customers (
    customer_id  BIGINT        NOT NULL AUTO_INCREMENT,
    email        VARCHAR(255)  NOT NULL,
    name         VARCHAR(255)  NOT NULL,
    created_at   TIMESTAMP     NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (customer_id),
    UNIQUE KEY uq_customers_email (email)
) ENGINE=InnoDB;

-- ============================================================
-- TABLE 2: products
-- ============================================================
CREATE TABLE IF NOT EXISTS products (
    product_id    BIGINT          NOT NULL AUTO_INCREMENT,
    sku           VARCHAR(100)    NOT NULL,
    name          VARCHAR(255)    NOT NULL,
    current_price DECIMAL(10, 2)  NOT NULL,
    in_stock      TINYINT(1)      NOT NULL DEFAULT 1,

    PRIMARY KEY (product_id),
    UNIQUE KEY uq_products_sku (sku),
    CONSTRAINT chk_products_price CHECK (current_price >= 0)
) ENGINE=InnoDB;

-- ============================================================
-- TABLE 3: carts
-- Represents both the active cart and checkout history.
-- One customer can have many carts over time (1:N).
-- The current basket is the row with status = 'active'.
-- ============================================================
CREATE TABLE IF NOT EXISTS carts (
    cart_id      BIGINT       NOT NULL AUTO_INCREMENT,
    customer_id  BIGINT       NOT NULL,
    status       VARCHAR(20)  NOT NULL DEFAULT 'active',
    created_at   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,

    PRIMARY KEY (cart_id),
    CONSTRAINT fk_carts_customer
        FOREIGN KEY (customer_id) REFERENCES customers(customer_id)
        ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT chk_carts_status
        CHECK (status IN ('active', 'checked_out', 'abandoned'))
) ENGINE=InnoDB;

-- Index: customer history queries  →  SELECT * FROM carts WHERE customer_id = ?
CREATE INDEX idx_carts_customer_id ON carts(customer_id);

-- Index: active cart lookup  →  WHERE customer_id = ? AND status = 'active'
-- MySQL 8.0 does not support partial indexes; the composite covers both columns.
CREATE INDEX idx_carts_customer_status ON carts(customer_id, status);

-- ============================================================
-- TABLE 4: cart_items
-- Each row is one product line inside a cart.
-- unit_price is a snapshot — records the price at add-time so
-- a later price change does not silently alter cart totals.
-- ============================================================
CREATE TABLE IF NOT EXISTS cart_items (
    cart_item_id  BIGINT          NOT NULL AUTO_INCREMENT,
    cart_id       BIGINT          NOT NULL,
    product_id    BIGINT          NOT NULL,
    quantity      INT             NOT NULL,
    unit_price    DECIMAL(10, 2)  NOT NULL,
    added_at      TIMESTAMP       NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (cart_item_id),
    -- Prevents duplicate product rows in the same cart.
    -- The INSERT ... ON DUPLICATE KEY UPDATE pattern relies on this.
    UNIQUE KEY uq_cart_product (cart_id, product_id),
    CONSTRAINT fk_cart_items_cart
        FOREIGN KEY (cart_id) REFERENCES carts(cart_id)
        ON DELETE CASCADE ON UPDATE CASCADE,
    CONSTRAINT fk_cart_items_product
        FOREIGN KEY (product_id) REFERENCES products(product_id)
        ON DELETE RESTRICT ON UPDATE CASCADE,
    CONSTRAINT chk_cart_items_quantity  CHECK (quantity > 0),
    CONSTRAINT chk_cart_items_price     CHECK (unit_price >= 0)
) ENGINE=InnoDB;

-- Index: cart retrieval JOIN  →  JOIN cart_items ON cart_id = ?
-- The UNIQUE KEY uq_cart_product already covers cart_id as its left column,
-- so a separate index on cart_id alone is redundant — MySQL uses it automatically.

-- Index: reverse FK lookup  →  find carts referencing a product (product updates/deletes)
CREATE INDEX idx_cart_items_product_id ON cart_items(product_id);
