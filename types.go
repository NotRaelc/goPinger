package main

import (
	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgxpool"
)

var pool *pgxpool.Pool
var validate *validator.Validate

type RegisterUser struct {
	Username        string `json:"username" validate:"required,min=1"`
	Password        string `json:"password" validate:"required,min=1"`
	PasswordConfirm string `json:"password_confirm" validate:"required,eqfield=Password"`
}
type LoginUser struct {
	Username string `json:"username" validate:"required,min=1"`
	Password string `json:"password" validate:"required,min=1"`
}

type User struct {
	Username string `json:"username" validate:"required,min=1"`
	Hash     string `json:"password" validate:"required,min=1"`
	Token    string `json:"token" validate:"required,min=1"`
}
