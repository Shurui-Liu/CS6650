# Week 8 Implementation Notes
## Shopping Cart API with MySQL

---

## Database Schema Design Decisions

### Table Structure
The schema uses four tables: `customers`, `products`, `carts`, and `cart_items`.

The key design decision was making `carts` a 1:N relationship with `customers` rather than 1:1. A 1:1 model (one cart per customer) would be simpler but destroys purchase history on checkout. The assignment explicitly requires customer history queries, which forces the 1:N design — each checkout creates a new cart row with `status = 'checked_out'`, preserving all past orders.

`cart_items` stores `unit_price` as a snapshot rather than referencing the product's `current_price`. This prevents a product price change from silently altering the total of a cart that was built at a different price.

### Key Strategy
- `BIGINT AUTO_INCREMENT` primary keys — smaller index footprint than UUIDs, clustered by insertion order which matches retrieval patterns
- `ON DELETE CASCADE` on `cart_items.cart_id` — deleting a cart atomically removes all its items
- `ON DELETE RESTRICT` on FK references to `customers` and `products` — prevents accidental deletion of records with live dependencies
- `UNIQUE KEY uq_cart_product (cart_id, product_id)` on `cart_items` — enforces one row per product per cart at the database level, enables the `ON DUPLICATE KEY UPDATE` upsert pattern

### Index Strategy
- `idx_carts_customer_id` — covers customer history queries
- `idx_carts_customer_status (customer_id, status)` — composite covers active-cart lookups; MySQL uses the left prefix for `WHERE customer_id = ? AND status = 'active'`
- `idx_cart_items_product_id` — covers FK reverse lookups on product
- The `UNIQUE KEY uq_cart_product` doubles as the cart retrieval index — MySQL uses its left prefix `cart_id` for `JOIN cart_items ON cart_id = ?`, making a separate single-column index redundant

---

## Key Challenges with MySQL Integration

### 1. Schema Translation from PostgreSQL
The initial schema design used PostgreSQL syntax. MySQL 8.0 required several translations: `GENERATED ALWAYS AS IDENTITY` → `AUTO_INCREMENT`, `TIMESTAMPTZ` → `TIMESTAMP`, partial indexes (unsupported) → composite indexes, and `ON CONFLICT DO UPDATE` → `ON DUPLICATE KEY UPDATE`. The composite index replacing the partial index is slightly less efficient (larger, covers more rows) but functionally equivalent for the access pattern.

### 2. Container Crash on Missing Schema
The application called `log.Fatalf` if the database ping failed at startup. Since the schema had not been applied when ECS first deployed the new image, every container crashed immediately. ECS fell back to the previous task definition, causing the new `/shopping-carts` routes to return 404. Fixed by downgrading the DB ping failure to a warning log, allowing the container to start and serve requests even if the DB is temporarily unreachable.

### 3. Stale Image in ECR
Code changes were made on a local Windows machine but the Docker image was built in AWS CloudShell, which had the old `main.go`. The new routes were never compiled into the deployed image. Caught by the persistent 404 responses on `/shopping-carts` after what appeared to be a successful deployment.

### 4. Rolling Deployment Split Traffic
After pushing the correct image, approximately 50% of requests still returned 404. ECS was mid-deployment with both old and new tasks running simultaneously, and the ALB load-balanced between them in round-robin. Required waiting for the old task to fully drain before running the test.

---

## Performance Observations

### Test Results (150 operations, same-region CloudShell → ALB)

| Operation    | Avg    | Median | P95    | Max    |
|--------------|--------|--------|--------|--------|
| create_cart  | ~25ms  | ~24ms  | ~31ms  | ~40ms  |
| add_items    | ~28ms  | ~27ms  | ~35ms  | ~45ms  |
| get_cart     | ~24ms  | ~23ms  | ~30ms  | ~38ms  |

All 150 operations completed in ~18 seconds (well within the 5-minute limit). All 150 succeeded once tested from us-west-2 (same region as the ALB).

`add_items` is consistently the slowest operation because it runs four statements inside a transaction: `BEGIN`, `SELECT FOR UPDATE` (row lock), `INSERT ON DUPLICATE KEY UPDATE`, and `UPDATE carts SET updated_at`. The `COMMIT` adds a synchronous InnoDB log flush before returning, which the other operations do not incur.

### Connection Pool Behavior
Pool configured at `MaxOpenConns=25`, `MaxIdleConns=10`, `ConnMaxLifetime=5m`. At 50 sequential operations, the pool never reached its limit — CloudWatch showed a peak of 2-3 DB connections during the test. The pool would become relevant under true concurrent load (100+ simultaneous HTTP handlers). Sizing at 25 rather than 100 (1:1 with HTTP concurrency) is intentional: each handler holds a connection only during the actual query, not for the full request lifetime.

---

## Week 5 In-Memory vs Week 8 MySQL Comparison

| Dimension         | Week 5 (In-Memory)              | Week 8 (MySQL RDS)                     |
|-------------------|---------------------------------|----------------------------------------|
| Storage           | `sync.RWMutex` + Go slice       | MySQL 8.0, InnoDB, db.t3.micro         |
| Avg latency       | ~3ms                            | ~25ms                                  |
| P95 latency       | ~8ms                            | ~35ms                                  |
| Concurrency       | RLock (reads non-blocking)      | Row-level locking, connection pool     |
| Persistence       | None (lost on restart)          | Durable (InnoDB WAL)                   |
| History queries   | Not possible                    | Full purchase history via `status`     |
| Failure mode      | Memory exhaustion                | Connection pool exhaustion, RDS limits |

The ~22ms overhead of MySQL over in-memory is almost entirely network round-trip (client → ALB → ECS → RDS) plus InnoDB's write-ahead log flush on commit. The actual query execution inside MySQL on a primary-key lookup is sub-millisecond.

The in-memory approach is faster but fundamentally unsuitable for a production cart system: data is lost on every deployment or container crash, concurrent writes require a global lock that serializes all mutations regardless of which customer they affect, and there is no way to query historical orders. MySQL's row-level locking means two users modifying different carts have zero contention, while the in-memory `sync.Mutex` would serialize them.
