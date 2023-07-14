# Bakery Service

![Bakery Service](./images/bakery-frontend.png)

The Bakery Service is a gRPC server written in Go that manages several operations for a virtual bakery shop. The server provides functionality for checking inventory, making bread, buying bread, and removing old bread. It uses RabbitMQ for asynchronous message passing and PostgreSQL for persistent data storage.
## Author
- [Carlos Alvarado Martínez](https://calvarado04.com)

## Table of Contents
- [Dependencies](#dependencies)
- [Setup](#setup)
- [Usage](#usage)
- [Bakery Server Endpoints](#bakery-server-endpoints)
- [Troubleshooting](#troubleshooting)
- [License](#license)

## Dependencies

1. **Go**: The language used to develop this application. Ensure you have Go installed on your system.
2. **gRPC**: Used for handling remote procedure calls.
3. **RabbitMQ**: Used for message queueing. This allows the bakery service to consume and publish messages asynchronously.
4. **PostgreSQL**: The database used for data persistence.

## Setup

The Bakery Service needs a running RabbitMQ instance and PostgreSQL database.

Set the following environment variables:

- `BAKERY_SERVICE_ADDR`: The address for the Bakery Service gRPC server
- `RABBITMQ_SERVICE_ADDR`: The address for the RabbitMQ server
- `DSN`: The PostgreSQL database connection string

Install the Go dependencies with:

```bash
go mod download
```

Start the application with:

```bash
go run main.go
```

## Usage

The Bakery Service runs several background tasks to handle the bakery operations:

- **Check Bread**: Every 30 seconds, the bakery checks the bread inventory and sends a message to the RabbitMQ queue make-bread-order if more bread needs to be made.
- **Perform Buy Bread**: The service consumes messages from the RabbitMQ queue buy-bread-order and processes the buying operation.
- **gRPC Server**: The gRPC server listens for incoming requests and handles them accordingly.

## Bakery Server Endpoints
The Bakery Service provides several gRPC endpoints:

- **CheckInventoryServer**: Checks the current inventory of the bakery.
- **MakeBreadServer**: Makes bread based on the given order.
- **BuyBreadServer**: Processes the purchase of bread.
- **RemoveOldBreadServer**: Removes old bread from the inventory.

## Troubleshooting
If you face any issues while setting up or running the Bakery Service, here are a few things to check:

1. RabbitMQ Connection: Make sure your RabbitMQ server is running and accessible from the Bakery Service.
2. PostgreSQL Connection: The Bakery Service needs to connect to a PostgreSQL database. Ensure it is running and accessible, and the connection string is correct.
3. Environment Variables: Verify that all necessary environment variables are set.

## License
[GPLv3](https://www.gnu.org/licenses/gpl-3.0.en.html)
