package data

type Repository interface {
	InsertCustomer(customer Customer) (int, error)
	InsertBread(bread Bread) (int, error)
	InsertBuyOrder(order BuyOrder) (int, error)
	InsertBreadMaker(baker BreadMaker) (int, error)
	InsertMakeOrder(order MakeOrder) (int, error)
	PasswordMatches(plainText string, customer Customer) (bool, error)
}
