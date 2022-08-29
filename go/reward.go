package main

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

// reward ゲーム報酬受取
// POST /user/{userID}/reward
func (h *Handler) reward(c echo.Context) error {
	userID, err := getUserID(c)
	if err != nil {
		return errorResponse(c, http.StatusBadRequest, err)
	}

	// parse body
	defer c.Request().Body.Close()
	req := new(RewardRequest)
	if err := parseRequestBody(c, req); err != nil {
		return errorResponse(c, http.StatusBadRequest, err)
	}

	requestAt, err := getRequestTime(c)
	if err != nil {
		return errorResponse(c, http.StatusInternalServerError, ErrGetRequestTime)
	}

	if err = h.checkViewerID(c, userID, req.ViewerID); err != nil {
		if err == ErrUserDeviceNotFound {
			return errorResponse(c, http.StatusNotFound, err)
		}
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	// 最後に取得した報酬時刻取得
	user := new(User)
	query := "SELECT * FROM users WHERE id=?"
	if err = c.Get("db").(*sqlx.DB).Get(user, query, userID); err != nil {
		if err == sql.ErrNoRows {
			return errorResponse(c, http.StatusNotFound, ErrUserNotFound)
		}
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	// 使っているデッキの取得
	deck := new(UserDeck)
	query = "SELECT * FROM user_decks WHERE user_id=? AND deleted_at IS NULL"
	if err = c.Get("db").(*sqlx.DB).Get(deck, query, userID); err != nil {
		if err == sql.ErrNoRows {
			return errorResponse(c, http.StatusNotFound, err)
		}
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	cards := make([]*UserCard, 0)
	query = "SELECT * FROM user_cards WHERE id IN (?, ?, ?)"
	if err = c.Get("db").(*sqlx.DB).Select(&cards, query, deck.CardID1, deck.CardID2, deck.CardID3); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}
	if len(cards) != 3 {
		return errorResponse(c, http.StatusBadRequest, fmt.Errorf("invalid cards length"))
	}

	// 経過時間*生産性のcoin (1椅子 = 1coin)
	pastTime := requestAt - user.LastGetRewardAt
	getCoin := int(pastTime) * (cards[0].AmountPerSec + cards[1].AmountPerSec + cards[2].AmountPerSec)

	// 報酬の保存(ゲームない通貨を保存)(users)
	user.IsuCoin += int64(getCoin)
	user.LastGetRewardAt = requestAt

	query = "UPDATE users SET isu_coin=?, last_getreward_at=? WHERE id=?"
	if _, err = c.Get("db").(*sqlx.DB).Exec(query, user.IsuCoin, user.LastGetRewardAt, user.ID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	return successResponse(c, &RewardResponse{
		UpdatedResources: makeUpdatedResources(requestAt, user, nil, nil, nil, nil, nil, nil),
	})
}

type RewardRequest struct {
	ViewerID string `json:"viewerId"`
}

type RewardResponse struct {
	UpdatedResources *UpdatedResource `json:"updatedResources"`
}
