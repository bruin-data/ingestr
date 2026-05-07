#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
BINARY="$PROJECT_ROOT/bin/gong"

SOURCE_URI="postgres+cdc://source_user:source_pass@localhost:5433/source_db?publication=test_pub"
DEST_URI="postgres://dest_user:dest_pass@localhost:5434/dest_db"
STRATEGY="merge"

log() {
    echo -e "${BLUE}[$(date '+%H:%M:%S')]${NC} $1"
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

step() {
    echo ""
    echo -e "${YELLOW}========================================${NC}"
    echo -e "${YELLOW}$1${NC}"
    echo -e "${YELLOW}========================================${NC}"
}

cleanup() {
    log "Cleaning up Docker containers..."
    cd "$SCRIPT_DIR"
    docker compose down -v 2>/dev/null || true
}

# Trap to cleanup on exit
trap cleanup EXIT

# Build the binary first
step "Building gong binary"
cd "$PROJECT_ROOT"
make build
success "Binary built at $BINARY"

# Start Docker containers
step "Starting Docker containers"
cd "$SCRIPT_DIR"
docker compose down -v 2>/dev/null || true
docker compose up -d

log "Waiting for containers to be healthy..."
sleep 5

# Wait for source postgres
for i in {1..30}; do
    if docker exec cdc_source_postgres pg_isready -U source_user -d source_db >/dev/null 2>&1; then
        success "Source Postgres is ready"
        break
    fi
    sleep 1
done

# Wait for dest postgres
for i in {1..30}; do
    if docker exec cdc_dest_postgres pg_isready -U dest_user -d dest_db >/dev/null 2>&1; then
        success "Destination Postgres is ready"
        break
    fi
    sleep 1
done

# Create tables and insert initial data in source
step "Creating source tables with 100 rows each"
docker exec cdc_source_postgres psql -U source_user -d source_db << 'EOSQL'
-- Create tables
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(200) NOT NULL,
    age INTEGER,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    product_id INTEGER NOT NULL,
    quantity INTEGER NOT NULL,
    total_price DECIMAL(10, 2) NOT NULL,
    status VARCHAR(50) DEFAULT 'pending',
    order_date TIMESTAMP DEFAULT NOW()
);

CREATE TABLE products (
    id SERIAL PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    description TEXT,
    price DECIMAL(10, 2) NOT NULL,
    stock INTEGER DEFAULT 0,
    category VARCHAR(100),
    is_active BOOLEAN DEFAULT true
);

-- Insert 100 users
INSERT INTO users (name, email, age)
SELECT
    'User ' || i,
    'user' || i || '@example.com',
    20 + (i % 50)
FROM generate_series(1, 100) AS i;

-- Insert 100 orders
INSERT INTO orders (user_id, product_id, quantity, total_price, status)
SELECT
    (i % 100) + 1,
    (i % 50) + 1,
    (i % 5) + 1,
    (RANDOM() * 1000)::DECIMAL(10, 2),
    CASE (i % 4)
        WHEN 0 THEN 'pending'
        WHEN 1 THEN 'shipped'
        WHEN 2 THEN 'delivered'
        ELSE 'cancelled'
    END
FROM generate_series(1, 100) AS i;

-- Insert 100 products
INSERT INTO products (name, description, price, stock, category, is_active)
SELECT
    'Product ' || i,
    'Description for product ' || i || '. This is a great product.',
    (RANDOM() * 500 + 10)::DECIMAL(10, 2),
    (RANDOM() * 1000)::INTEGER,
    CASE (i % 5)
        WHEN 0 THEN 'Electronics'
        WHEN 1 THEN 'Clothing'
        WHEN 2 THEN 'Books'
        WHEN 3 THEN 'Home'
        ELSE 'Sports'
    END,
    i % 10 != 0  -- 10% are inactive
FROM generate_series(1, 100) AS i;

-- Create publication for CDC
CREATE PUBLICATION test_pub FOR TABLE users, orders, products;

SELECT 'Tables created and populated' as status;
SELECT 'users: ' || COUNT(*) FROM users;
SELECT 'orders: ' || COUNT(*) FROM orders;
SELECT 'products: ' || COUNT(*) FROM products;
EOSQL

success "Source tables created with 100 rows each"

# Run first ingestion (snapshot)
step "Running first ingestion (should do full snapshot)"
log "Source URI: $SOURCE_URI"
log "Dest URI: $DEST_URI"

$BINARY ingest \
    --source-uri "$SOURCE_URI" \
    --dest-uri "$DEST_URI" \
    --incremental-strategy "$STRATEGY" \
    --yes \
    --debug

success "First ingestion completed"

# Validate after snapshot
step "Validating data after snapshot"
python3 "$SCRIPT_DIR/validate.py" "After Initial Snapshot" users orders products

success "Validation passed after snapshot"

# Make some updates in source
step "Making updates in source database"
docker exec cdc_source_postgres psql -U source_user -d source_db << 'EOSQL'
-- Update some users
UPDATE users SET name = 'Updated User ' || id, age = age + 1 WHERE id <= 10;

-- Update some orders
UPDATE orders SET status = 'completed', total_price = total_price * 1.1 WHERE id <= 15;

-- Update some products
UPDATE products SET price = price * 0.9, stock = stock + 100 WHERE id <= 20;

-- Insert a few new rows
INSERT INTO users (name, email, age) VALUES
    ('New User 101', 'newuser101@example.com', 25),
    ('New User 102', 'newuser102@example.com', 30),
    ('New User 103', 'newuser103@example.com', 35);

INSERT INTO orders (user_id, product_id, quantity, total_price, status) VALUES
    (101, 1, 2, 150.00, 'pending'),
    (102, 2, 1, 75.50, 'pending');

INSERT INTO products (name, description, price, stock, category) VALUES
    ('New Product 101', 'Brand new product', 199.99, 50, 'Electronics'),
    ('New Product 102', 'Another new product', 49.99, 200, 'Books');

SELECT 'Updates complete' as status;
SELECT 'users: ' || COUNT(*) FROM users;
SELECT 'orders: ' || COUNT(*) FROM orders;
SELECT 'products: ' || COUNT(*) FROM products;
EOSQL

success "Source data updated"

# Small delay to ensure WAL is flushed
sleep 2

# Run second ingestion (should resume from LSN, no snapshot)
step "Running second ingestion (should resume from LSN, NO snapshot)"
log "This should only capture the changes made above"

$BINARY ingest \
    --source-uri "$SOURCE_URI" \
    --dest-uri "$DEST_URI" \
    --incremental-strategy "$STRATEGY" \
    --yes \
    --debug

success "Second ingestion completed"

# Validate after incremental update
step "Validating data after incremental update"
python3 "$SCRIPT_DIR/validate.py" "After Incremental Update 1" users orders products

success "Validation passed after first incremental update"

# Make more changes
step "Making more changes in source database"
docker exec cdc_source_postgres psql -U source_user -d source_db << 'EOSQL'
-- More updates
UPDATE users SET email = 'updated_' || email WHERE id BETWEEN 50 AND 60;

-- Delete some rows (CDC should capture these)
DELETE FROM orders WHERE id > 100;

-- Update products
UPDATE products SET is_active = false WHERE category = 'Books';

-- Insert more
INSERT INTO users (name, email, age) VALUES
    ('Final User 104', 'final104@example.com', 40),
    ('Final User 105', 'final105@example.com', 45);

SELECT 'Final updates complete' as status;
SELECT 'users: ' || COUNT(*) FROM users;
SELECT 'orders: ' || COUNT(*) FROM orders;
SELECT 'products: ' || COUNT(*) FROM products;
EOSQL

success "More source data changes made"
sleep 2

# Run third ingestion
step "Running third ingestion (another incremental)"
$BINARY ingest \
    --source-uri "$SOURCE_URI" \
    --dest-uri "$DEST_URI" \
    --incremental-strategy "$STRATEGY" \
    --yes \
    --debug

success "Third ingestion completed"

# Final validation
step "Final validation after all changes"
python3 "$SCRIPT_DIR/validate.py" "After Incremental Update 2" users orders products

success "Final validation passed"

# Show CDC metadata summary
step "CDC Metadata Summary"
docker exec cdc_dest_postgres psql -U dest_user -d dest_db << 'EOSQL'
SELECT 'Users CDC Info:' as info;
SELECT COUNT(*) as total, COUNT(DISTINCT "_cdc_lsn") as distinct_lsns, SUM(CASE WHEN "_cdc_deleted" THEN 1 ELSE 0 END) as deleted FROM users;

SELECT 'Orders CDC Info:' as info;
SELECT COUNT(*) as total, COUNT(DISTINCT "_cdc_lsn") as distinct_lsns, SUM(CASE WHEN "_cdc_deleted" THEN 1 ELSE 0 END) as deleted FROM orders;

SELECT 'Products CDC Info:' as info;
SELECT COUNT(*) as total, COUNT(DISTINCT "_cdc_lsn") as distinct_lsns, SUM(CASE WHEN "_cdc_deleted" THEN 1 ELSE 0 END) as deleted FROM products;
EOSQL

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}ALL TESTS PASSED SUCCESSFULLY!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
