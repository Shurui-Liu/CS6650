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
                data = resp.json()
                # Flag as failure if response looks wrong
                if "products" not in data:
                    resp.failure("Missing 'products' key in response")
                else:
                    resp.success()
            else:
                resp.failure(f"HTTP {resp.status_code}")

    @task(1)
    def health_check(self):
        """Periodically check /health so we can see when the service dies."""
        with self.client.get("/health", catch_response=True, name="/health") as resp:
            if resp.status_code != 200:
                resp.failure(f"Health check failed: {resp.status_code}")

# ── How to run ─────────────────────────────────────────────────────────────────
#
# Test 1 — Baseline (healthy enrichment service):
#   locust -f locustfile.py --host http://localhost:8080 \
#          --users 5 --spawn-rate 1 --run-time 2m --headless
#
# Test 2 — Breaking point (flip chaos service to slow DURING this test):
#   locust -f locustfile.py --host http://localhost:8080 \
#          --users 20 --spawn-rate 2 --run-time 3m --headless
#
# Or use the Locust web UI (remove --headless) at http://localhost:8089