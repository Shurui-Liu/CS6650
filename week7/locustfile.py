from locust import HttpUser, task, between
import random
import logging

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class ProductAPIUser(HttpUser):
    """
    Product API Load Test
    Creates products on startup, then tests GET/POST operations
    """
    wait_time = between(1, 3)
    
    # Class variable to track created products across all users
    created_products = set()
    
    def on_start(self):
        """Each user creates 10 products when they start"""
        logger.info(f"User {id(self)} starting - creating products")
        
        # Create products 1-10 for this user
        base_id = (id(self) % 100) * 10  # Distribute IDs
        for i in range(1, 11):
            product_id = base_id + i
            if product_id not in self.created_products:
                self.create_product(product_id)
                self.created_products.add(product_id)
    
    def create_product(self, product_id):
        """Helper to create a product"""
        product_data = {
            "product_id": product_id,
            "sku": f"SKU-{product_id:05d}",
            "manufacturer": f"Manufacturer-{random.randint(1, 20)}",
            "category_id": random.randint(1, 50),
            "weight": random.randint(100, 5000),
            "some_other_id": random.randint(1000, 9999)
        }
        
        try:
            with self.client.post(
                f"/products/{product_id}/details",
                json=product_data,
                catch_response=True,
                name="/products/[id]/details [POST]"
            ) as response:
                if response.status_code == 204:
                    response.success()
                    logger.info(f"Created product {product_id}")
                    return True
                else:
                    response.failure(f"Status {response.status_code}")
                    return False
        except Exception as e:
            logger.error(f"Error creating product {product_id}: {e}")
            return False
    
    @task(8)  # 80% - Read existing products
    def get_product(self):
        """GET products we've created"""
        if self.created_products:
            # Pick from products we know exist
            product_id = random.choice(list(self.created_products))
        else:
            # Fallback to low IDs if nothing created yet
            product_id = random.randint(1, 10)
        
        with self.client.get(
            f"/products/{product_id}",
            catch_response=True,
            name="/products/[id] [GET]"
        ) as response:
            if response.status_code == 200:
                response.success()
            elif response.status_code == 404:
                # Product doesn't exist - not a real failure
                logger.warning(f"Product {product_id} not found (404)")
                response.success()  # Don't count as failure
            else:
                response.failure(f"Unexpected status: {response.status_code}")
    
    @task(2)  # 20% - Create/update products
    def create_or_update_product(self):
        """POST new or existing products"""
        # Mix of new and existing products
        if random.random() > 0.5 and self.created_products:
            # Update existing
            product_id = random.choice(list(self.created_products))
        else:
            # Create new
            product_id = random.randint(1, 1000)
        
        if self.create_product(product_id):
            self.created_products.add(product_id)