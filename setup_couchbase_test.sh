#!/bin/bash

# Couchbase Test Environment Setup Script
# This script spins up a Couchbase container and populates it with sample data

set -e

echo "🚀 Starting Couchbase Test Environment Setup..."

# Configuration
CONTAINER_NAME="couchbase-test"
COUCHBASE_USER="Administrator"
COUCHBASE_PASSWORD="password"
BUCKET_NAME="test_bucket"
BUCKET_RAM_QUOTA=256
DATA_RAM_QUOTA=512
INDEX_RAM_QUOTA=256

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if Docker is running
if ! docker info > /dev/null 2>&1; then
    echo "❌ Error: Docker is not running. Please start Docker and try again."
    exit 1
fi

# Stop and remove existing container if it exists
if docker ps -a | grep -q $CONTAINER_NAME; then
    echo "${YELLOW}⚠️  Removing existing container...${NC}"
    docker rm -f $CONTAINER_NAME > /dev/null 2>&1 || true
fi

# Start Couchbase container
echo "${BLUE}📦 Starting Couchbase Community 7.2.0 container...${NC}"
docker run -d \
    --name $CONTAINER_NAME \
    -p 8091-8096:8091-8096 \
    -p 11210:11210 \
    -p 11207:11207 \
    couchbase:community-7.2.0

# Wait for Couchbase to start
echo "${BLUE}⏳ Waiting for Couchbase to start (this may take 30-60 seconds)...${NC}"
sleep 15

# Function to check if Couchbase is ready
check_couchbase_ready() {
    docker logs $CONTAINER_NAME 2>&1 | grep -q "Starting Couchbase Server"
}

# Wait until Couchbase is ready
COUNTER=0
MAX_WAIT=60
until check_couchbase_ready || [ $COUNTER -eq $MAX_WAIT ]; do
    echo "  Waiting for Couchbase to be ready... ($COUNTER/$MAX_WAIT)"
    sleep 2
    COUNTER=$((COUNTER + 1))
done

if [ $COUNTER -eq $MAX_WAIT ]; then
    echo "❌ Timeout waiting for Couchbase to start"
    docker logs $CONTAINER_NAME
    exit 1
fi

echo "${GREEN}✓ Couchbase started${NC}"
sleep 5

# Initialize the cluster
echo "${BLUE}🔧 Initializing Couchbase cluster...${NC}"

# Setup memory quotas
curl -s -X POST http://localhost:8091/pools/default \
    -d memoryQuota=$DATA_RAM_QUOTA \
    -d indexMemoryQuota=$INDEX_RAM_QUOTA \
    > /dev/null

sleep 2

# Setup services
curl -s -X POST http://localhost:8091/node/controller/setupServices \
    -d 'services=kv,n1ql,index' \
    > /dev/null

sleep 2

# Setup admin credentials
curl -s -X POST http://localhost:8091/settings/web \
    -d "username=$COUCHBASE_USER" \
    -d "password=$COUCHBASE_PASSWORD" \
    -d port=8091 \
    > /dev/null

echo "${GREEN}✓ Cluster initialized${NC}"
sleep 5

# Create bucket
echo "${BLUE}🗄️  Creating bucket '$BUCKET_NAME'...${NC}"
curl -s -X POST http://localhost:8091/pools/default/buckets \
    -u $COUCHBASE_USER:$COUCHBASE_PASSWORD \
    -d "name=$BUCKET_NAME" \
    -d "ramQuota=$BUCKET_RAM_QUOTA" \
    -d "bucketType=couchbase" \
    > /dev/null

echo "${GREEN}✓ Bucket created${NC}"
sleep 10

# Wait for bucket to be ready
echo "${BLUE}⏳ Waiting for bucket to be ready...${NC}"
COUNTER=0
MAX_WAIT=30
until curl -s -u $COUCHBASE_USER:$COUCHBASE_PASSWORD http://localhost:8091/pools/default/buckets/$BUCKET_NAME | grep -q "healthy" || [ $COUNTER -eq $MAX_WAIT ]; do
    echo "  Waiting for bucket... ($COUNTER/$MAX_WAIT)"
    sleep 2
    COUNTER=$((COUNTER + 1))
done

echo "${GREEN}✓ Bucket is ready${NC}"
sleep 5

# Create primary index
echo "${BLUE}📑 Creating primary index...${NC}"
curl -s -X POST http://localhost:8093/query/service \
    -u $COUCHBASE_USER:$COUCHBASE_PASSWORD \
    -d "statement=CREATE PRIMARY INDEX ON \`$BUCKET_NAME\`" \
    > /dev/null

echo "${GREEN}✓ Primary index created${NC}"
sleep 3

# Insert sample data using Python
echo "${BLUE}📝 Inserting sample data...${NC}"

python3 << 'PYTHON_SCRIPT'
from couchbase.cluster import Cluster
from couchbase.auth import PasswordAuthenticator
from couchbase.options import ClusterOptions
from datetime import datetime, timedelta
import time

# Connection settings
auth = PasswordAuthenticator("Administrator", "password")
cluster = Cluster("couchbase://localhost", ClusterOptions(auth))

# Wait for connection
time.sleep(2)
cluster.wait_until_ready(timedelta(seconds=30))

bucket = cluster.bucket("test_bucket")
collection = bucket.default_collection()

# Sample data: Users
users = [
    {
        "type": "user",
        "user_id": 1,
        "name": "Alice Johnson",
        "email": "alice@example.com",
        "age": 28,
        "country": "United States",
        "status": "active",
        "created_at": (datetime.now() - timedelta(days=365)).isoformat(),
        "last_login": datetime.now().isoformat(),
    },
    {
        "type": "user",
        "user_id": 2,
        "name": "Bob Smith",
        "email": "bob@example.com",
        "age": 35,
        "country": "United Kingdom",
        "status": "active",
        "created_at": (datetime.now() - timedelta(days=180)).isoformat(),
        "last_login": (datetime.now() - timedelta(days=1)).isoformat(),
    },
    {
        "type": "user",
        "user_id": 3,
        "name": "Charlie Brown",
        "email": "charlie@example.com",
        "age": 42,
        "country": "Canada",
        "status": "inactive",
        "created_at": (datetime.now() - timedelta(days=730)).isoformat(),
        "last_login": (datetime.now() - timedelta(days=90)).isoformat(),
    },
    {
        "type": "user",
        "user_id": 4,
        "name": "Diana Prince",
        "email": "diana@example.com",
        "age": 31,
        "country": "United States",
        "status": "active",
        "created_at": (datetime.now() - timedelta(days=90)).isoformat(),
        "last_login": datetime.now().isoformat(),
    },
    {
        "type": "user",
        "user_id": 5,
        "name": "Eve Wilson",
        "email": "eve@example.com",
        "age": 26,
        "country": "Australia",
        "status": "active",
        "created_at": (datetime.now() - timedelta(days=30)).isoformat(),
        "last_login": datetime.now().isoformat(),
    },
]

# Sample data: Orders
orders = [
    {
        "type": "order",
        "order_id": 1001,
        "user_id": 1,
        "product": "Laptop",
        "amount": 1299.99,
        "status": "completed",
        "order_date": (datetime.now() - timedelta(days=10)).isoformat(),
    },
    {
        "type": "order",
        "order_id": 1002,
        "user_id": 1,
        "product": "Mouse",
        "amount": 29.99,
        "status": "completed",
        "order_date": (datetime.now() - timedelta(days=8)).isoformat(),
    },
    {
        "type": "order",
        "order_id": 1003,
        "user_id": 2,
        "product": "Keyboard",
        "amount": 79.99,
        "status": "completed",
        "order_date": (datetime.now() - timedelta(days=5)).isoformat(),
    },
    {
        "type": "order",
        "order_id": 1004,
        "user_id": 4,
        "product": "Monitor",
        "amount": 399.99,
        "status": "pending",
        "order_date": (datetime.now() - timedelta(days=2)).isoformat(),
    },
    {
        "type": "order",
        "order_id": 1005,
        "user_id": 5,
        "product": "Webcam",
        "amount": 89.99,
        "status": "completed",
        "order_date": (datetime.now() - timedelta(days=1)).isoformat(),
    },
]

# Sample data: Products
products = [
    {
        "type": "product",
        "product_id": "P001",
        "name": "Laptop",
        "category": "Electronics",
        "price": 1299.99,
        "stock": 15,
        "rating": 4.5,
    },
    {
        "type": "product",
        "product_id": "P002",
        "name": "Mouse",
        "category": "Electronics",
        "price": 29.99,
        "stock": 150,
        "rating": 4.2,
    },
    {
        "type": "product",
        "product_id": "P003",
        "name": "Keyboard",
        "category": "Electronics",
        "price": 79.99,
        "stock": 75,
        "rating": 4.3,
    },
    {
        "type": "product",
        "product_id": "P004",
        "name": "Monitor",
        "category": "Electronics",
        "price": 399.99,
        "stock": 25,
        "rating": 4.7,
    },
    {
        "type": "product",
        "product_id": "P005",
        "name": "Webcam",
        "category": "Electronics",
        "price": 89.99,
        "stock": 50,
        "rating": 4.1,
    },
]

# Insert users
for user in users:
    collection.upsert(f"user::{user['user_id']}", user)

# Insert orders
for order in orders:
    collection.upsert(f"order::{order['order_id']}", order)

# Insert products
for product in products:
    collection.upsert(f"product::{product['product_id']}", product)

print(f"✓ Inserted {len(users)} users")
print(f"✓ Inserted {len(orders)} orders")
print(f"✓ Inserted {len(products)} products")

PYTHON_SCRIPT

if [ $? -eq 0 ]; then
    echo "${GREEN}✓ Sample data inserted successfully${NC}"
else
    echo "${YELLOW}⚠️  Warning: Could not insert sample data (couchbase Python package may not be installed)${NC}"
    echo "${YELLOW}   Install with: pip install couchbase${NC}"
fi

# Print connection information
echo ""
echo "${GREEN}========================================${NC}"
echo "${GREEN}✅ Couchbase Test Environment Ready!${NC}"
echo "${GREEN}========================================${NC}"
echo ""
echo "📋 Connection Details:"
echo "   Web Console: http://localhost:8091"
echo "   Username: $COUCHBASE_USER"
echo "   Password: $COUCHBASE_PASSWORD"
echo "   Bucket: $BUCKET_NAME"
echo ""
echo "🔌 Connection Strings:"
echo "   Standard: couchbase://$COUCHBASE_USER:$COUCHBASE_PASSWORD@localhost:11210/$BUCKET_NAME"
echo "   For ingestr: couchbase://$COUCHBASE_USER:$COUCHBASE_PASSWORD@localhost:11210/$BUCKET_NAME"
echo ""
echo "📊 Sample Data:"
echo "   - 5 users (type: user)"
echo "   - 5 orders (type: order)"
echo "   - 5 products (type: product)"
echo ""
echo "🧪 Test Query (via curl):"
echo "   curl -u $COUCHBASE_USER:$COUCHBASE_PASSWORD http://localhost:8093/query/service \\"
echo "     -d 'statement=SELECT * FROM \`$BUCKET_NAME\` WHERE type=\"user\" LIMIT 5'"
echo ""
echo "💡 Example ingestr command:"
echo "   ingestr ingest \\"
echo "     --source-uri 'couchbase://$COUCHBASE_USER:$COUCHBASE_PASSWORD@localhost:11210/$BUCKET_NAME' \\"
echo "     --source-table '_default' \\"
echo "     --dest-uri 'duckdb:///output.db' \\"
echo "     --dest-table 'couchbase_data'"
echo ""
echo "🛑 To stop: docker stop $CONTAINER_NAME"
echo "🗑️  To remove: docker rm $CONTAINER_NAME"
echo ""
