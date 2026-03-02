from locust import task, between
from locust.contrib.fasthttp import FastHttpUser

# Search terms that will reliably hit products across all categories
SEARCH_TERMS = [
    "Electronics",
    "Alpha",
    "Books",
    "Beta",
    "Home",
    "Gamma",
    "Sports",
    "Delta",
]

class ProductSearchUser(FastHttpUser):
    # Minimal wait — we want to hammer the service
    wait_time = between(0.1, 0.5)

    @task
    def search_products(self):
        import random
        term = random.choice(SEARCH_TERMS)
        with self.client.get(
            f"/products/search?q={term}",
            catch_response=True,
            name="/products/search"
        ) as resp:
            if resp.status_code == 200:
                resp.success()
            else:
                resp.failure(f"HTTP {resp.status_code}")

    @task(1)
    def health_check(self):
        """Periodically check /health so we can see when the service dies."""
        with self.client.get("/health", catch_response=True, name="/health") as resp:
            if resp.status_code == 200:
                resp.success()
            else:
                resp.failure(f"Health check failed: {resp.status_code}")

