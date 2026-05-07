CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(255) NOT NULL,
    age INTEGER,
    salary NUMERIC(10,2),
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO users (name, email, age, salary, is_active, created_at) VALUES
    ('Alice Johnson', 'alice@example.com', 28, 75000.00, true, '2024-01-15 10:30:00'),
    ('Bob Smith', 'bob@example.com', 35, 85000.50, true, '2024-01-16 11:45:00'),
    ('Charlie Brown', 'charlie@example.com', 42, 95000.75, false, '2024-02-01 09:00:00'),
    ('Diana Prince', 'diana@example.com', 31, 120000.00, true, '2024-02-10 14:20:00'),
    ('Eve Wilson', 'eve@example.com', 26, 65000.25, true, '2024-03-05 16:30:00'),
    ('Frank Miller', 'frank@example.com', 45, 110000.00, true, '2024-03-15 08:15:00'),
    ('Grace Lee', 'grace@example.com', 33, 88000.00, false, '2024-04-01 12:00:00'),
    ('Henry Davis', 'henry@example.com', 29, 72000.50, true, '2024-04-20 10:45:00'),
    ('Ivy Chen', 'ivy@example.com', 38, 98000.00, true, '2024-05-10 15:30:00'),
    ('Jack Taylor', 'jack@example.com', 41, 105000.25, true, '2024-05-25 09:30:00');

CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    product_name VARCHAR(200) NOT NULL,
    quantity INTEGER NOT NULL,
    price NUMERIC(10,2) NOT NULL,
    status VARCHAR(50) DEFAULT 'pending',
    order_date DATE NOT NULL,
    shipped_at TIMESTAMP
);

INSERT INTO orders (user_id, product_name, quantity, price, status, order_date, shipped_at) VALUES
    (1, 'Laptop Pro 15', 1, 1299.99, 'delivered', '2024-01-20', '2024-01-25 14:00:00'),
    (1, 'Wireless Mouse', 2, 49.99, 'delivered', '2024-02-15', '2024-02-18 10:30:00'),
    (2, 'Mechanical Keyboard', 1, 159.99, 'delivered', '2024-02-01', '2024-02-05 11:00:00'),
    (3, 'Monitor 27 inch', 2, 399.99, 'shipped', '2024-03-10', '2024-03-15 09:00:00'),
    (4, 'USB-C Hub', 1, 79.99, 'delivered', '2024-03-20', '2024-03-23 16:00:00'),
    (5, 'Webcam HD', 1, 129.99, 'pending', '2024-04-01', NULL),
    (6, 'Standing Desk', 1, 599.99, 'delivered', '2024-04-15', '2024-04-22 12:00:00'),
    (7, 'Laptop Pro 15', 1, 1299.99, 'cancelled', '2024-05-01', NULL),
    (8, 'Wireless Earbuds', 3, 199.99, 'shipped', '2024-05-10', '2024-05-14 08:00:00'),
    (9, 'Tablet 10 inch', 1, 449.99, 'pending', '2024-05-20', NULL),
    (10, 'Phone Case', 5, 29.99, 'delivered', '2024-05-25', '2024-05-27 15:30:00'),
    (1, 'External SSD 1TB', 1, 149.99, 'delivered', '2024-06-01', '2024-06-04 10:00:00');
