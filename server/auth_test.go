package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/metadata"
)

// --- helpers ---

func newAuthServer(repo data.Repository) *AuthServiceServer {
	return &AuthServiceServer{
		RabbitMQBakery: &RabbitMQBakery{
			Config: Config{Repo: repo},
		},
	}
}

// mustHashPassword hashes a password for use in tests.
func mustHashPassword(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	return string(hash)
}

// adminUserRepo returns a specific admin user by username.
type adminUserRepo struct {
	stubRepo
	user data.AdminUser
}

func (r *adminUserRepo) GetAdminUserByUsername(username string) (data.AdminUser, error) {
	if username != r.user.Username {
		return data.AdminUser{}, errors.New("user not found")
	}
	return r.user, nil
}

// noAdminRepo always fails GetAdminUserByUsername.
type noAdminRepo struct{ stubRepo }

func (r *noAdminRepo) GetAdminUserByUsername(string) (data.AdminUser, error) {
	return data.AdminUser{}, errors.New("user not found")
}

// customerEmailRepo returns a specific customer by email.
type customerEmailRepo struct {
	stubRepo
	customer data.Customer
}

func (r *customerEmailRepo) GetCustomerByEmail(email string) (data.Customer, error) {
	if email != r.customer.Email {
		return data.Customer{}, errors.New("customer not found")
	}
	return r.customer, nil
}

// noCustomerRepo always fails GetCustomerByEmail.
type noCustomerRepo struct{ stubRepo }

func (r *noCustomerRepo) GetCustomerByEmail(string) (data.Customer, error) {
	return data.Customer{}, errors.New("customer not found")
}

// insertAdminRepo tracks InsertAdminUser calls.
type insertAdminRepo struct {
	stubRepo
	returnID int
	err      error
}

func (r *insertAdminRepo) InsertAdminUser(u data.AdminUser) (int, error) {
	return r.returnID, r.err
}

// --- AdminLogin tests ---

func TestAdminLogin_Success(t *testing.T) {
	hash := mustHashPassword(t, "secret123")
	repo := &adminUserRepo{
		user: data.AdminUser{
			ID:        1,
			Username:  "admin",
			Email:     "admin@bakery.com",
			Password:  hash,
			Role:      "superadmin",
			CreatedAt: time.Now(),
		},
	}
	srv := newAuthServer(repo)

	resp, err := srv.AdminLogin(context.Background(), &pb.LoginRequest{
		Username: "admin",
		Password: "secret123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success=true, got false: %s", resp.Message)
	}
	if resp.Token == "" {
		t.Error("expected non-empty JWT token")
	}
	if resp.User == nil || resp.User.Username != "admin" {
		t.Error("expected admin user in response")
	}
	if resp.User.Role != "superadmin" {
		t.Errorf("expected role=superadmin, got %s", resp.User.Role)
	}
}

func TestAdminLogin_WrongPassword(t *testing.T) {
	hash := mustHashPassword(t, "correct_pass")
	repo := &adminUserRepo{
		user: data.AdminUser{
			ID:       1,
			Username: "admin",
			Password: hash,
		},
	}
	srv := newAuthServer(repo)

	resp, err := srv.AdminLogin(context.Background(), &pb.LoginRequest{
		Username: "admin",
		Password: "wrong_pass",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Error("expected success=false for wrong password")
	}
	if resp.Token != "" {
		t.Error("expected empty token on failed login")
	}
}

func TestAdminLogin_UserNotFound(t *testing.T) {
	srv := newAuthServer(&noAdminRepo{})

	resp, err := srv.AdminLogin(context.Background(), &pb.LoginRequest{
		Username: "nobody",
		Password: "whatever",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Error("expected success=false for non-existent user")
	}
}

// --- CustomerLogin tests ---

func TestCustomerLogin_Success(t *testing.T) {
	hash := mustHashPassword(t, "mypassword")
	repo := &customerEmailRepo{
		customer: data.Customer{
			ID:        5,
			Name:      "Jane Doe",
			Email:     "jane@doe.com",
			Password:  hash,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	srv := newAuthServer(repo)

	resp, err := srv.CustomerLogin(context.Background(), &pb.CustomerLoginRequest{
		Email:    "jane@doe.com",
		Password: "mypassword",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success=true, got false: %s", resp.Message)
	}
	if resp.Token == "" {
		t.Error("expected non-empty JWT token")
	}
	if resp.Customer == nil || resp.Customer.Email != "jane@doe.com" {
		t.Error("expected customer in response")
	}
}

func TestCustomerLogin_WrongPassword(t *testing.T) {
	hash := mustHashPassword(t, "correct")
	repo := &customerEmailRepo{
		customer: data.Customer{
			ID:       5,
			Email:    "jane@doe.com",
			Password: hash,
		},
	}
	srv := newAuthServer(repo)

	resp, err := srv.CustomerLogin(context.Background(), &pb.CustomerLoginRequest{
		Email:    "jane@doe.com",
		Password: "wrong",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Error("expected success=false for wrong password")
	}
}

func TestCustomerLogin_EmailNotFound(t *testing.T) {
	srv := newAuthServer(&noCustomerRepo{})

	resp, err := srv.CustomerLogin(context.Background(), &pb.CustomerLoginRequest{
		Email:    "ghost@nowhere.com",
		Password: "pass",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Success {
		t.Error("expected success=false for non-existent email")
	}
}

// --- ValidateToken tests ---

func TestValidateToken_ValidAdminToken(t *testing.T) {
	hash := mustHashPassword(t, "pass")
	repo := &adminUserRepo{
		user: data.AdminUser{ID: 1, Username: "admin", Password: hash, Role: "admin"},
	}
	srv := newAuthServer(repo)

	loginResp, err := srv.AdminLogin(context.Background(), &pb.LoginRequest{
		Username: "admin", Password: "pass",
	})
	if err != nil || !loginResp.Success {
		t.Fatalf("login failed unexpectedly: %v / success=%v", err, loginResp.GetSuccess())
	}

	valResp, err := srv.ValidateToken(context.Background(), &pb.ValidateTokenRequest{
		Token: loginResp.Token,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valResp.Valid {
		t.Error("expected valid=true for fresh token")
	}
	if valResp.UserType != "admin" {
		t.Errorf("expected user_type=admin, got %s", valResp.UserType)
	}
	if valResp.UserId == "" {
		t.Error("expected non-empty user_id")
	}
}

func TestValidateToken_ValidCustomerToken(t *testing.T) {
	hash := mustHashPassword(t, "cpass")
	repo := &customerEmailRepo{
		customer: data.Customer{ID: 7, Name: "Jane", Email: "jane@test.com", Password: hash, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}
	srv := newAuthServer(repo)

	loginResp, err := srv.CustomerLogin(context.Background(), &pb.CustomerLoginRequest{
		Email: "jane@test.com", Password: "cpass",
	})
	if err != nil || !loginResp.Success {
		t.Fatalf("customer login failed: %v", err)
	}

	valResp, err := srv.ValidateToken(context.Background(), &pb.ValidateTokenRequest{
		Token: loginResp.Token,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valResp.Valid {
		t.Error("expected valid=true for customer token")
	}
	if valResp.UserType != "customer" {
		t.Errorf("expected user_type=customer, got %s", valResp.UserType)
	}
}

func TestValidateToken_GarbageToken(t *testing.T) {
	srv := newAuthServer(&stubRepo{})

	resp, err := srv.ValidateToken(context.Background(), &pb.ValidateTokenRequest{
		Token: "this.is.definitely.not.a.valid.jwt",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Valid {
		t.Error("expected valid=false for garbage token")
	}
}

func TestValidateToken_EmptyToken(t *testing.T) {
	srv := newAuthServer(&stubRepo{})

	resp, err := srv.ValidateToken(context.Background(), &pb.ValidateTokenRequest{
		Token: "",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Valid {
		t.Error("expected valid=false for empty token")
	}
}

// adminCtx returns a context carrying a valid admin Bearer token signed with
// the same jwtSecret used by the server under test.
func adminCtx() context.Context {
	claims := &Claims{
		UserID:   1,
		Username: "testadmin",
		UserType: "admin",
		Role:     "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
	md := metadata.Pairs("authorization", "Bearer "+token)
	return metadata.NewIncomingContext(context.Background(), md)
}

// --- CreateAdminUser tests ---

func TestCreateAdminUser_Success(t *testing.T) {
	srv := newAuthServer(&insertAdminRepo{returnID: 42})

	result, err := srv.CreateAdminUser(adminCtx(), &pb.CreateAdminUserRequest{
		Username: "newadmin",
		Email:    "new@bakery.com",
		Password: "securepass",
		Role:     "admin",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Id != 42 {
		t.Errorf("expected ID=42, got %d", result.Id)
	}
	if result.Username != "newadmin" {
		t.Errorf("expected username=newadmin, got %s", result.Username)
	}
	if result.Role != "admin" {
		t.Errorf("expected role=admin, got %s", result.Role)
	}
}

func TestCreateAdminUser_DBError(t *testing.T) {
	srv := newAuthServer(&insertAdminRepo{err: errors.New("unique constraint violation")})

	_, err := srv.CreateAdminUser(adminCtx(), &pb.CreateAdminUserRequest{
		Username: "duplicate",
		Email:    "dup@bakery.com",
		Password: "pass",
		Role:     "admin",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
