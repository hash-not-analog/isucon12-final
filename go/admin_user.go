package main

import (
	"database/sql"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	"github.com/logica0419/helpisu"
)

// adminUser ユーザの詳細画面
// GET /admin/user/{userID}
func (h *Handler) adminUser(c echo.Context) error {
	userID, err := getUserID(c)
	if err != nil {
		return errorResponse(c, http.StatusBadRequest, err)
	}

	query := "SELECT * FROM users WHERE id=?"
	user := new(User)
	if err = c.Get("db").(*sqlx.DB).Get(user, query, userID); err != nil {
		if err == sql.ErrNoRows {
			return errorResponse(c, http.StatusNotFound, ErrUserNotFound)
		}
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	query = "SELECT * FROM user_devices WHERE user_id=?"
	devices := make([]*UserDevice, 0)
	if err = c.Get("db").(*sqlx.DB).Select(&devices, query, userID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	query = "SELECT * FROM user_cards WHERE user_id=?"
	cards := make([]*UserCard, 0)
	if err = c.Get("db").(*sqlx.DB).Select(&cards, query, userID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	query = "SELECT * FROM user_decks WHERE user_id=?"
	decks := make([]*UserDeck, 0)
	if err = c.Get("db").(*sqlx.DB).Select(&decks, query, userID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	query = "SELECT * FROM user_items WHERE user_id=?"
	items := make([]*UserItem, 0)
	if err = c.Get("db").(*sqlx.DB).Select(&items, query, userID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	query = "SELECT * FROM user_login_bonuses WHERE user_id=?"
	loginBonuses := make([]*UserLoginBonus, 0)
	if err = c.Get("db").(*sqlx.DB).Select(&loginBonuses, query, userID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	query = "SELECT * FROM user_presents WHERE user_id=?"
	presents := make([]*UserPresent, 0)
	if err = c.Get("db").(*sqlx.DB).Select(&presents, query, userID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	query = "SELECT * FROM user_present_all_received_history WHERE user_id=?"
	presentHistory := make([]*UserPresentAllReceivedHistory, 0)
	if err = c.Get("db").(*sqlx.DB).Select(&presentHistory, query, userID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	return successResponse(c, &AdminUserResponse{
		User:                          user,
		UserDevices:                   devices,
		UserCards:                     cards,
		UserDecks:                     decks,
		UserItems:                     items,
		UserLoginBonuses:              loginBonuses,
		UserPresents:                  presents,
		UserPresentAllReceivedHistory: presentHistory,
	})
}

type AdminUserResponse struct {
	User *User `json:"user"`

	UserDevices                   []*UserDevice                    `json:"userDevices"`
	UserCards                     []*UserCard                      `json:"userCards"`
	UserDecks                     []*UserDeck                      `json:"userDecks"`
	UserItems                     []*UserItem                      `json:"userItems"`
	UserLoginBonuses              []*UserLoginBonus                `json:"userLoginBonuses"`
	UserPresents                  []*UserPresent                   `json:"userPresents"`
	UserPresentAllReceivedHistory []*UserPresentAllReceivedHistory `json:"userPresentAllReceivedHistory"`
}

// adminBanUser ユーザBAN処理
// POST /admin/user/{userId}/ban
var userBanCache = helpisu.NewCache[int64, UserBan]()

func (h *Handler) adminBanUser(c echo.Context) error {
	userID, err := getUserID(c)
	if err != nil {
		return errorResponse(c, http.StatusBadRequest, err)
	}

	requestAt, err := getRequestTime(c)
	if err != nil {
		return errorResponse(c, http.StatusInternalServerError, ErrGetRequestTime)
	}

	query := "SELECT * FROM users WHERE id=?"
	user := new(User)
	if err = c.Get("db").(*sqlx.DB).Get(user, query, userID); err != nil {
		if err == sql.ErrNoRows {
			return errorResponse(c, http.StatusBadRequest, ErrUserNotFound)
		}
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	banID, err := h.generateID()
	if err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}
	query = "INSERT user_bans(id, user_id, created_at, updated_at) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE updated_at = ?"
	if _, err = c.Get("db").(*sqlx.DB).Exec(query, banID, userID, requestAt, requestAt, requestAt); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	return successResponse(c, &AdminBanUserResponse{
		User: user,
	})
}

type AdminBanUserResponse struct {
	User *User `json:"user"`
}
