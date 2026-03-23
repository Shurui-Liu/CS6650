"""
Shopping Cart Performance Test
Runs exactly 150 operations: 50 create, 50 add_items, 50 get_cart
Saves results to mysql_test_results.json
"""

import json
import time
import requests
from datetime import datetime, timezone

# ── Config ────────────────────────────────────────────────────────────────────
BASE_URL = "http://product-api-service-alb-2056062177.us-west-2.elb.amazonaws.com"   # replace with ALB DNS after terraform apply
CUSTOMER_ID = 1                      # must exist in customers table (see seed.sql)
RESULTS_FILE = "dynamodb_test_results.json"

# ── Helpers ───────────────────────────────────────────────────────────────────

def record(operation, response, elapsed_ms):
    return {
        "operation":     operation,
        "response_time": round(elapsed_ms, 2),
        "success":       response.status_code < 400,
        "status_code":   response.status_code,
        "timestamp":     datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    }

def timed_post(url, body):
    start = time.perf_counter()
    resp  = requests.post(url, json=body, timeout=10)
    ms    = (time.perf_counter() - start) * 1000
    return resp, ms

def timed_get(url):
    start = time.perf_counter()
    resp  = requests.get(url, timeout=10)
    ms    = (time.perf_counter() - start) * 1000
    return resp, ms

# ── Phases ────────────────────────────────────────────────────────────────────

def phase_create(n=50):
    print(f"Phase 1: creating {n} carts...")
    results  = []
    cart_ids = []
    for i in range(n):
        resp, ms = timed_post(f"{BASE_URL}/dynamo/shopping-carts", {"customer_id": CUSTOMER_ID})
        results.append(record("create_cart", resp, ms))
        if resp.status_code == 201:
            cart_ids.append(resp.json()["cart_id"])
        else:
            print(f"  [!] create {i+1} failed {resp.status_code}: {resp.text[:80]}")
    ok = sum(1 for r in results if r["success"])
    print(f"  done — {ok}/{n} succeeded\n")
    return results, cart_ids


def phase_add_items(cart_ids):
    n = len(cart_ids)
    print(f"Phase 2: adding items to {n} carts...")
    results = []
    for i, cart_id in enumerate(cart_ids):
        body = {
            "product_id": (i % 3) + 1,   # cycles through products 1-3 from seed.sql
            "quantity":   (i % 5) + 1,
            "unit_price": round(9.99 + i * 0.50, 2),
        }
        resp, ms = timed_post(f"{BASE_URL}/dynamo/shopping-carts/{cart_id}/items", body)
        results.append(record("add_items", resp, ms))
        if resp.status_code not in (200, 201):
            print(f"  [!] add_items cart {cart_id} failed {resp.status_code}: {resp.text[:80]}")
    ok = sum(1 for r in results if r["success"])
    print(f"  done — {ok}/{n} succeeded\n")
    return results


def phase_get(cart_ids):
    n = len(cart_ids)
    print(f"Phase 3: retrieving {n} carts...")
    results = []
    for cart_id in cart_ids:
        resp, ms = timed_get(f"{BASE_URL}/dynamo/shopping-carts/{cart_id}")
        results.append(record("get_cart", resp, ms))
        if resp.status_code != 200:
            print(f"  [!] get cart {cart_id} failed {resp.status_code}: {resp.text[:80]}")
    ok = sum(1 for r in results if r["success"])
    print(f"  done — {ok}/{n} succeeded\n")
    return results

# ── Summary ───────────────────────────────────────────────────────────────────

def print_summary(all_results):
    by_op = {}
    for r in all_results:
        by_op.setdefault(r["operation"], []).append(r)

    print("=" * 50)
    print(f"{'Operation':<15} {'Count':>5}  {'OK':>4}  {'Avg ms':>7}  {'Max ms':>7}")
    print("-" * 50)
    for op, rows in by_op.items():
        times = [r["response_time"] for r in rows]
        ok    = sum(1 for r in rows if r["success"])
        print(f"{op:<15} {len(rows):>5}  {ok:>4}  {sum(times)/len(times):>7.1f}  {max(times):>7.1f}")
    print("=" * 50)
    print(f"Total: {len(all_results)} operations")

# ── Main ──────────────────────────────────────────────────────────────────────

def main():
    suite_start = time.perf_counter()
    print(f"Target: {BASE_URL}\n")

    create_results, cart_ids = phase_create(50)

    if not cart_ids:
        print("No carts created — aborting. Check BASE_URL and that the service is running.")
        return

    # Use however many carts were actually created (up to 50) for the next phases
    cart_ids = cart_ids[:50]

    add_results = phase_add_items(cart_ids)
    get_results = phase_get(cart_ids)

    all_results = create_results + add_results + get_results

    with open(RESULTS_FILE, "w") as f:
        json.dump(all_results, f, indent=2)

    elapsed = time.perf_counter() - suite_start
    print(f"\nResults saved to {RESULTS_FILE}")
    print(f"Total elapsed: {elapsed:.1f}s  (limit: 300s)\n")
    print_summary(all_results)

    if elapsed > 300:
        print("\n[!] WARNING: test exceeded 5-minute window")


if __name__ == "__main__":
    main()
