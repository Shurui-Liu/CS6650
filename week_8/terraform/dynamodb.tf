# ==========================================
# DynamoDB — Shopping Carts (NoSQL)
# ==========================================
#
# Single-table design. Each cart is one DynamoDB item.
# Items are stored as a Map attribute — no JOIN needed,
# single GetItem retrieves the full cart.
#
# PK:  cart_id     (String, UUID) — even distribution, no hot partitions
# GSI: customer_id + created_at  — customer history queries
# TTL: ttl attribute             — auto-expires abandoned carts at no cost

resource "aws_dynamodb_table" "shopping_carts" {
  name         = "shopping-carts"
  billing_mode = "PAY_PER_REQUEST"  # on-demand: no capacity planning, scales automatically
  hash_key     = "cart_id"

  attribute {
    name = "cart_id"
    type = "S"
  }

  # GSI attributes must be declared even though other attributes are schemaless
  attribute {
    name = "customer_id"
    type = "N"
  }

  attribute {
    name = "created_at"
    type = "S"
  }

  # ── TTL ───────────────────────────────────────────────────────────────────
  # DynamoDB deletes items whose ttl value (epoch seconds) is in the past.
  # Free — does not consume write capacity. Keeps table lean for long-running
  # deployments where abandoned carts would otherwise accumulate.

  ttl {
    attribute_name = "ttl"
    enabled        = true
  }

  # ── GSI: customer history ─────────────────────────────────────────────────
  # Supports: "get all carts for customer X, newest first"
  # PK  = customer_id  (fan-out per customer — no hot partition risk at scale)
  # SK  = created_at   (ISO8601 string sorts chronologically)

  global_secondary_index {
    name            = "customer-index"
    hash_key        = "customer_id"
    range_key       = "created_at"
    projection_type = "ALL"
  }

  point_in_time_recovery {
    enabled = false  # off for assignment/dev — enable in production
  }

  tags = {
    Name        = "shopping-carts"
    Environment = "dev"
    Purpose     = "NoSQL cart storage for MySQL comparison"
  }
}
