from locust import task, between
from locust.contrib.fasthttp import FastHttpUser

class SearchUser(FastHttpUser):
    wait_time = between(0.1, 0.5)

    @task
    def search(self):
        terms = ["Alpha", "Electronics", "Beta", "Books", "Gamma"]
        import random
        self.client.get(f"/products/search?q={random.choice(terms)}")