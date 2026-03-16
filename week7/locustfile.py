from locust import HttpUser, task, between
import random


class OrderAsyncUser(HttpUser):
    wait_time = between(0.1, 0.5)

    @task
    def post_order_async(self):
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
            "/orders/async",
            json=payload,
            catch_response=True,
            name="/orders/async [POST]",
        ) as response:
            if response.status_code == 202:
                response.success()
            else:
                response.failure(f"Status {response.status_code}")