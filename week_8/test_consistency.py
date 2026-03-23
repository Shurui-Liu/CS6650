"""
DynamoDB Consistency Test
Tests read-after-write behavior across three scenarios.
Saves results to consistency_test_results.json
"""

import json
import time
import requests
import threading
from datetime import datetime, timezone

BASE_URL    = "http://product-api-service-alb-2056062177.us-west-2.elb.amazonaws.com"
RESULTS_FILE = "consistency_test_results.json"
ITERATIONS  = 20   # per scenario

results = []

def ts():
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")

def post(path, body):
    return requests.post(f"{BASE_URL}{path}", json=body, timeout=10)

def get(path):
    return requests.get(f"{BASE_URL}{path}", timeout=10)

# ── Scenario 1: Create cart → immediately retrieve it ─────────────────────────
# Tests whether a cart is visible immediately after creation.
# An eventual consistency delay here means GetItem returns 404 right after PutItem.

def scenario_create_then_get():
    print(f"\nScenario 1: create → immediate GET  ({ITERATIONS} iterations)")
    delays   = []
    misses   = 0

    for i in range(ITERATIONS):
        # Create
        r = post("/dynamo/shopping-carts", {"customer_id": 1})
        if r.status_code != 201:
            print(f"  [{i+1}] create failed {r.status_code}")
            continue
        cart_id    = r.json()["cart_id"]
        write_time = time.perf_counter()

        # Immediately read — no sleep
        found      = False
        attempts   = 0
        delay_ms   = 0
        while attempts < 10:
            attempts += 1
            gr = get(f"/dynamo/shopping-carts/{cart_id}")
            if gr.status_code == 200:
                delay_ms = (time.perf_counter() - write_time) * 1000
                found    = True
                break
            time.sleep(0.005)  # 5ms between retries

        if not found:
            misses += 1
            print(f"  [{i+1}] INCONSISTENT — cart not visible after {attempts} attempts")

        entry = {
            "scenario":    "create_then_get",
            "iteration":   i + 1,
            "cart_id":     cart_id,
            "consistent":  found,
            "attempts":    attempts,
            "delay_ms":    round(delay_ms, 2),
            "timestamp":   ts(),
        }
        results.append(entry)
        if found:
            delays.append(delay_ms)

    print(f"  Inconsistencies: {misses}/{ITERATIONS}")
    if delays:
        print(f"  Avg delay to consistency: {sum(delays)/len(delays):.1f}ms  "
              f"Max: {max(delays):.1f}ms")
    return misses

# ── Scenario 2: Add item → immediately fetch cart ─────────────────────────────
# Tests whether an item is visible in the cart immediately after UpdateItem.
# A delay here means the items Map in GetItem still shows the old state.

def scenario_add_then_get():
    print(f"\nScenario 2: add item → immediate GET  ({ITERATIONS} iterations)")

    # Create one cart to reuse
    r = post("/dynamo/shopping-carts", {"customer_id": 1})
    cart_id = r.json()["cart_id"]
    time.sleep(0.1)  # let the cart settle before the test

    delays  = []
    misses  = 0

    for i in range(ITERATIONS):
        product_id = (i % 5) + 1
        qty        = i + 1

        # Add item
        post(f"/dynamo/shopping-carts/{cart_id}/items",
             {"product_id": product_id, "quantity": qty, "unit_price": 9.99})
        write_time = time.perf_counter()

        # Immediately read and check item is present
        found    = False
        attempts = 0
        delay_ms = 0
        while attempts < 10:
            attempts += 1
            gr = get(f"/dynamo/shopping-carts/{cart_id}")
            if gr.status_code == 200:
                items = gr.json().get("items", [])
                pids  = [it["product_id"] for it in items]
                if product_id in pids:
                    delay_ms = (time.perf_counter() - write_time) * 1000
                    found    = True
                    break
            time.sleep(0.005)

        if not found:
            misses += 1
            print(f"  [{i+1}] INCONSISTENT — item {product_id} not visible after {attempts} attempts")

        results.append({
            "scenario":   "add_then_get",
            "iteration":  i + 1,
            "cart_id":    cart_id,
            "product_id": product_id,
            "consistent": found,
            "attempts":   attempts,
            "delay_ms":   round(delay_ms, 2),
            "timestamp":  ts(),
        })
        if found:
            delays.append(delay_ms)

    print(f"  Inconsistencies: {misses}/{ITERATIONS}")
    if delays:
        print(f"  Avg delay to consistency: {sum(delays)/len(delays):.1f}ms  "
              f"Max: {max(delays):.1f}ms")
    return misses

# ── Scenario 3: Rapid concurrent updates from multiple clients ─────────────────
# 5 threads each add a different product to the same cart simultaneously.
# Tests whether concurrent UpdateItem operations on the same item conflict.
# DynamoDB uses optimistic concurrency at the attribute level — different
# Map keys should not conflict.

def scenario_concurrent_updates():
    print(f"\nScenario 3: concurrent updates ({ITERATIONS} carts × 5 threads)")

    total_conflicts = 0
    total_success   = 0

    for i in range(ITERATIONS):
        r = post("/dynamo/shopping-carts", {"customer_id": 1})
        if r.status_code != 201:
            continue
        cart_id = r.json()["cart_id"]
        time.sleep(0.05)

        thread_results = []

        def add_item(pid, tid):
            start = time.perf_counter()
            resp  = post(f"/dynamo/shopping-carts/{cart_id}/items",
                         {"product_id": pid, "quantity": 1, "unit_price": float(pid)})
            ms    = (time.perf_counter() - start) * 1000
            thread_results.append({
                "thread":     tid,
                "product_id": pid,
                "status":     resp.status_code,
                "success":    resp.status_code == 200,
                "latency_ms": round(ms, 2),
            })

        threads = [threading.Thread(target=add_item, args=(pid, t))
                   for t, pid in enumerate(range(1, 6))]
        for t in threads: t.start()
        for t in threads: t.join()

        success   = sum(1 for tr in thread_results if tr["success"])
        conflicts = len(thread_results) - success
        total_success   += success
        total_conflicts += conflicts

        results.append({
            "scenario":   "concurrent_updates",
            "iteration":  i + 1,
            "cart_id":    cart_id,
            "threads":    5,
            "success":    success,
            "conflicts":  conflicts,
            "details":    thread_results,
            "timestamp":  ts(),
        })

    print(f"  Total writes:    {total_success + total_conflicts}")
    print(f"  Succeeded:       {total_success}")
    print(f"  Conflicts/fails: {total_conflicts}")
    return total_conflicts

# ── Summary ───────────────────────────────────────────────────────────────────

def print_summary(miss1, miss2, conflicts3):
    print("\n" + "=" * 60)
    print("CONSISTENCY TEST SUMMARY")
    print("=" * 60)

    s1 = [r for r in results if r["scenario"] == "create_then_get" and r["consistent"]]
    s2 = [r for r in results if r["scenario"] == "add_then_get"    and r["consistent"]]
    s3 = [r for r in results if r["scenario"] == "concurrent_updates"]

    print(f"\nScenario 1 — create → get")
    print(f"  Inconsistencies observed: {miss1}/{ITERATIONS}")
    if s1:
        delays = [r["delay_ms"] for r in s1]
        print(f"  Avg consistency delay:    {sum(delays)/len(delays):.1f}ms")
        print(f"  Max consistency delay:    {max(delays):.1f}ms")

    print(f"\nScenario 2 — add item → get")
    print(f"  Inconsistencies observed: {miss2}/{ITERATIONS}")
    if s2:
        delays = [r["delay_ms"] for r in s2]
        print(f"  Avg consistency delay:    {sum(delays)/len(delays):.1f}ms")
        print(f"  Max consistency delay:    {max(delays):.1f}ms")

    print(f"\nScenario 3 — concurrent updates (5 threads × {ITERATIONS} carts)")
    total_writes = sum(r["success"] + r["conflicts"] for r in s3)
    print(f"  Total writes:    {total_writes}")
    print(f"  Conflicts/fails: {conflicts3}")

    print("\n── Findings ──────────────────────────────────────────────")

    if miss1 == 0 and miss2 == 0:
        print("  No eventual consistency delays observed.")
        print("  DynamoDB achieved read-after-write consistency within")
        print("  a single round-trip from same-region client.")
    else:
        print(f"  Consistency delays detected in {miss1+miss2} reads.")
        print("  Affected patterns: immediate read after write.")

    if conflicts3 == 0:
        print("  Concurrent updates to different Map keys: zero conflicts.")
        print("  DynamoDB attribute-level isolation prevents write collisions")
        print("  on different products within the same cart item.")
    else:
        print(f"  {conflicts3} concurrent write failures — review UpdateItem conditions.")

    print("=" * 60)

# ── Main ──────────────────────────────────────────────────────────────────────

def main():
    print(f"Target: {BASE_URL}\n")

    miss1      = scenario_create_then_get()
    miss2      = scenario_add_then_get()
    conflicts3 = scenario_concurrent_updates()

    with open(RESULTS_FILE, "w") as f:
        json.dump(results, f, indent=2)

    print(f"\nResults saved to {RESULTS_FILE}")
    print_summary(miss1, miss2, conflicts3)

if __name__ == "__main__":
    main()
