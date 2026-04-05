# Bakery Service - Project Overview

## Introduction
The Bakery Service is a microservices-based application written in Go that manages operations for a virtual bakery shop. It uses gRPC for service communication, RabbitMQ for asynchronous message passing, and PostgreSQL for persistent data storage.

## Project Structure
```
bakery-go/
├── broker/                 # RabbitMQ message broker service
├── buyers/                 # Buyer service that sends purchase requests
├── data/                   # Data access layer and models
├── frontend/               # Web frontend application
├── makers/                 # Maker service that produces bread
├── proto/                  # gRPC service definitions
├── server/                 # Main gRPC server implementing all services
├── bakery.sql              # Database schema
├── broker.dockerfile       # Dockerfile for broker service
├── buyers.dockerfile       # Dockerfile for buyers service
├── frontend.dockerfile     # Dockerfile for frontend service
├── makers.dockerfile       # Dockerfile for makers service
├── server.dockerfile       # Dockerfile for server service
├── README.md               # Project documentation
└── go.mod                  # Go module dependencies
```

## Core Components

### 1. Data Layer (`data/`)
- **Models**: Defines data structures (Customer, Bread, BuyOrder, etc.)
- **Repository**: Interface and implementation for database operations
- **PostgreSQL**: Uses pgx driver for database connectivity

### 2. gRPC Services (`proto/bread.proto`)
Defines the following services:
- **MakeBread**: Handles bread production requests
- **CheckInventory**: Provides bread inventory information
- **BuyBread**: Processes bread purchases
- **RemoveOldBread**: Handles removal of stale bread
- **BuyOrderService**: Manages buy order operations
- **AdminService**: Administrative functions (dashboard, users, etc.)
- **AuthService**: Authentication for admin and customer users
- **InvoiceService**: Invoice generation and management
- **CustomerPortalService**: Customer-facing portal functions
- **MakeOrderService**: Handles make order operations

### 3. Services
- **Broker (`broker/`)**: Listens for buy-bread-order messages from RabbitMQ, processes purchases, and publishes bread-bought events
- **Buyers (`buyers/`)**: Sends gRPC requests to buy bread and streams responses
- **Makers (`makers/`)**: (Not examined but likely handles bread production)
- **Frontend (`frontend/`)**: Web application with admin and customer portals
- **Server (`server/`)**: Main gRPC server that registers all service implementations

### 4. Key Features
- **Asynchronous Processing**: Uses RabbitMQ for decoupling services
- **Real-time Updates**: gRPC streaming for live inventory and order updates
- **Database Persistence**: PostgreSQL for storing all business data
- **Web Interface**: Admin dashboard and customer portal
- **Authentication**: Secure login for admin and customer users
- **Invoice Generation**: Automatic invoice creation for purchases

## Data Flow
1. Buyers service sends gRPC BuyBread request to Server
2. Server publishes buy-bread-order message to RabbitMQ
3. Broker service consumes buy-bread-order messages, processes purchase:
   - Validates bread availability
   - Updates inventory quantities
   - Creates outbox message for confirmation
   - Publishes bread-bought message to RabbitMQ
4. Server consumes bread-bought messages and updates database
5. Frontend displays real-time updates via gRPC streams
6. Admin service manages inventory, users, orders, etc.

## Configuration
Environment variables:
- `BAKERY_SERVICE_ADDR`: gRPC server address (default: localhost:50051)
- `RABBITMQ_SERVICE_ADDR`: RabbitMQ connection string (default: amqp://guest:guest@localhost:5672/)
- `DSN`: PostgreSQL database connection string

## Running the Application
1. Set up PostgreSQL and RabbitMQ
2. Set environment variables
3. Run each service:
   - `go run broker/main.go`
   - `go run buyers/main.go`
   - `go run makers/main.go`
   - `go run server/main.go`
   - `go run frontend/cmd/web/main.go`

## Docker Deployment
Each service has its own Dockerfile for containerized deployment.

## Database Schema
See `bakery.sql` for complete schema including:
- Customers, Bread, BreadMakers tables
- BuyOrders, MakeOrders with details
- OrdersProcessed for completed transactions
- Outbox table for message reliability
- AdminUsers for authentication
- Invoices and InvoiceItems for billing

## Security Considerations
- Passwords stored using bcrypt hashing
- gRPC services can be secured with TLS (not implemented in current version)
- Input validation needed for production use