"""
Order Sync Load Test

Run with:
  Normal:  locust -f locustfile.py --host=http://localhost:8080 --spawn-rate 1
  Flash:   locust -f locustfile.py --host=http://localhost:8080 --spawn-rate 10

Spawn rate: 1 user/sec (normal), 10 users/sec (flash)
Wait time: random 100-500ms between requests
Endpoint: POST /orders/sync
"""

from locust import HttpUser, task, between
import random
import time


class OrderSyncUser(HttpUser):
    """User that repeatedly submits orders via POST /orders/sync."""

    # Random 100-500ms between requests
    wait_time = between(0.1, 0.5)

    @task
    def post_order_sync(self):
        """POST an order to /orders/sync (payment verification ~3s)."""
        payload = {
            "customer_id": random.randint(1, 10000),
            "items": [
                {
                    "product_id": random.randint(1, 100000),
                    "name": f"Product-{random.randint(1, 1000)}",
                    "quantity": random.randint(1, 5),
                    "price": round(random.uniform(4.99, 199.99), 2),
                }
                for _ in range(random.randint(1, 4))
            ],
        }
        with self.client.post(
            "/orders/sync",
            json=payload,
            catch_response=True,
            name="/orders/sync [POST]",
        ) as response:
            if response.status_code == 200:
                response.success()
            else:
                response.failure(f"Status {response.status_code}")
