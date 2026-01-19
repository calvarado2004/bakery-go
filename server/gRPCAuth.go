package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/calvarado2004/bakery-go/data"
	pb "github.com/calvarado2004/bakery-go/proto"
	"github.com/golang-jwt/jwt/v5"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var jwtSecret = []byte(getJWTSecret())

func getJWTSecret() string {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "bakery-go-secret-key-change-in-production"
	}
	return secret
}

type AuthServiceServer struct {
	pb.UnimplementedAuthServiceServer
	RabbitMQBakery *RabbitMQBakery
}

type Claims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	UserType string `json:"user_type"` // "admin" or "customer"
	Role     string `json:"role,omitempty"`
	jwt.RegisteredClaims
}

func (s *AuthServiceServer) AdminLogin(ctx context.Context, in *pb.LoginRequest) (*pb.LoginResponse, error) {
	user, err := s.RabbitMQBakery.Repo.GetAdminUserByUsername(in.Username)
	if err != nil {
		log.Errorf("Admin user not found: %v", err)
		return &pb.LoginResponse{
			Success: false,
			Message: "Invalid username or password",
		}, nil
	}

	// Check password
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(in.Password))
	if err != nil {
		log.Errorf("Invalid password for admin user: %v", err)
		return &pb.LoginResponse{
			Success: false,
			Message: "Invalid username or password",
		}, nil
	}

	// Generate JWT token
	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		UserType: "admin",
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "bakery-go",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		log.Errorf("Error generating token: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to generate token")
	}

	return &pb.LoginResponse{
		Success: true,
		Token:   tokenString,
		Message: "Login successful",
		User: &pb.AdminUser{
			Id:        int32(user.ID),
			Username:  user.Username,
			Email:     user.Email,
			Role:      user.Role,
			CreatedAt: user.CreatedAt.String(),
		},
	}, nil
}

func (s *AuthServiceServer) CustomerLogin(ctx context.Context, in *pb.CustomerLoginRequest) (*pb.CustomerLoginResponse, error) {
	customer, err := s.RabbitMQBakery.Repo.GetCustomerByEmail(in.Email)
	if err != nil {
		log.Errorf("Customer not found: %v", err)
		return &pb.CustomerLoginResponse{
			Success: false,
			Message: "Invalid email or password",
		}, nil
	}

	// Check password
	err = bcrypt.CompareHashAndPassword([]byte(customer.Password), []byte(in.Password))
	if err != nil {
		log.Errorf("Invalid password for customer: %v", err)
		return &pb.CustomerLoginResponse{
			Success: false,
			Message: "Invalid email or password",
		}, nil
	}

	// Generate JWT token
	expirationTime := time.Now().Add(24 * time.Hour)
	claims := &Claims{
		UserID:   customer.ID,
		Username: customer.Email,
		UserType: "customer",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "bakery-go",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		log.Errorf("Error generating token: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to generate token")
	}

	return &pb.CustomerLoginResponse{
		Success: true,
		Token:   tokenString,
		Message: "Login successful",
		Customer: &pb.Customer{
			Id:        int32(customer.ID),
			Name:      customer.Name,
			Email:     customer.Email,
			CreatedAt: customer.CreatedAt.String(),
			UpdatedAt: customer.UpdatedAt.String(),
		},
	}, nil
}

func (s *AuthServiceServer) ValidateToken(ctx context.Context, in *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(in.Token, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return jwtSecret, nil
	})

	if err != nil || !token.Valid {
		return &pb.ValidateTokenResponse{
			Valid: false,
		}, nil
	}

	return &pb.ValidateTokenResponse{
		Valid:    true,
		UserId:   strconv.Itoa(claims.UserID),
		UserType: claims.UserType,
	}, nil
}

func (s *AuthServiceServer) CreateAdminUser(ctx context.Context, in *pb.CreateAdminUserRequest) (*pb.AdminUser, error) {
	user := data.AdminUser{
		Username: in.Username,
		Email:    in.Email,
		Password: in.Password,
		Role:     in.Role,
	}

	newID, err := s.RabbitMQBakery.Repo.InsertAdminUser(user)
	if err != nil {
		log.Errorf("Error creating admin user: %v", err)
		return nil, status.Errorf(codes.Internal, "Failed to create admin user: %v", err)
	}

	return &pb.AdminUser{
		Id:        int32(newID),
		Username:  user.Username,
		Email:     user.Email,
		Role:      user.Role,
		CreatedAt: time.Now().String(),
	}, nil
}
