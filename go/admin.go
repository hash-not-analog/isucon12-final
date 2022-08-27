package main

import (
	"database/sql"
	"net/http"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

// //////////////////////////////////////
// admin

// adminSessionCheckMiddleware
func (h *Handler) adminSessionCheckMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		sessID := c.Request().Header.Get("x-session")

		adminSession := new(Session)
		query := "SELECT * FROM admin_sessions WHERE session_id=? AND deleted_at IS NULL"
		if err := h.DB.Get(adminSession, query, sessID); err != nil {
			if err == sql.ErrNoRows {
				return errorResponse(c, http.StatusUnauthorized, ErrUnauthorized)
			}
			return errorResponse(c, http.StatusInternalServerError, err)
		}

		requestAt, err := getRequestTime(c)
		if err != nil {
			return errorResponse(c, http.StatusInternalServerError, ErrGetRequestTime)
		}

		if adminSession.ExpiredAt < requestAt {
			query = "UPDATE admin_sessions SET deleted_at=? WHERE session_id=?"
			if _, err = h.DB.Exec(query, requestAt, sessID); err != nil {
				return errorResponse(c, http.StatusInternalServerError, err)
			}
			return errorResponse(c, http.StatusUnauthorized, ErrExpiredSession)
		}

		// next
		if err := next(c); err != nil {
			c.Error(err)
		}
		return nil
	}
}

//nolint:deadcode,unused
func hashPassword(pw string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", ErrGeneratePassword
	}
	return string(hash), nil
}

func verifyPassword(hash, pw string) error {
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)); err != nil {
		return ErrUnauthorized
	}
	return nil
}

type AdminUser struct {
	ID              int64  `db:"id"`
	Password        string `db:"password"`
	LastActivatedAt int64  `db:"last_activated_at"`
	CreatedAt       int64  `db:"created_at"`
	UpdatedAt       int64  `db:"updated_at"`
	DeletedAt       *int64 `db:"deleted_at"`
}
