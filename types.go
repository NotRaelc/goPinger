package main

import (
	"encoding/json"

	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5/pgxpool"

	"goPinger/query"
)

var (
	pool     *pgxpool.Pool
	validate *validator.Validate
)

type RegisterUser struct {
	Username        string `json:"username" validate:"required,min=1,max=32,alphanum"`
	Password        string `json:"password" validate:"required,min=8,max=128,printascii"`
	PasswordConfirm string `json:"password_confirm" validate:"required,eqfield=Password"`
}

type LoginUser struct {
	Username string `json:"username" validate:"required,min=1,max=32,alphanum"`
	Password string `json:"password" validate:"required,min=1,max=128,printascii"`
}

type User struct {
	Username string `json:"username"`
	Hash     string `json:"-"`
	Token    string `json:"-"`
}

type Server struct {
	ID          int64
	UserID      string
	Address     string
	Type        int
	IntervalSec int
	Label       string
	Enabled     bool
}

type ServerDTO struct {
	ID                   int64        `json:"id"`
	Address              string       `json:"address"`
	Type                 int          `json:"type"`
	IntervalSec          int          `json:"interval_sec"`
	EffectiveIntervalSec int          `json:"effective_interval_sec"`
	NextPollAt           int64        `json:"next_poll_at,omitempty"`
	Label                string       `json:"label"`
	Enabled              bool         `json:"enabled"`
	IconURL              string       `json:"icon_url,omitempty"`
	State                *ServerState `json:"state,omitempty"`
}

type ServerState struct {
	OK        bool          `json:"ok"`
	Error     string        `json:"error,omitempty"`
	CheckedAt int64         `json:"checked_at"`
	FromCache bool          `json:"from_cache,omitempty"`
	Payload   *query.Server `json:"payload,omitempty"`
}

type queryServer query.Server

func (q *queryServer) Unmarshal(b []byte) error { return json.Unmarshal(b, (*query.Server)(q)) }
func (q *queryServer) Ptr() *query.Server       { return (*query.Server)(q) }
