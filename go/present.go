package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

// listPresent プレゼント一覧
// GET /user/{userID}/present/index/{n}
func (h *Handler) listPresent(c echo.Context) error {
	n, err := strconv.Atoi(c.Param("n"))
	if err != nil {
		return errorResponse(c, http.StatusBadRequest, fmt.Errorf("invalid index number (n) parameter"))
	}
	if n == 0 {
		return errorResponse(c, http.StatusBadRequest, fmt.Errorf("index number (n) should be more than or equal to 1"))
	}

	userID, err := getUserID(c)
	if err != nil {
		return errorResponse(c, http.StatusBadRequest, fmt.Errorf("invalid userID parameter"))
	}

	offset := PresentCountPerPage * (n - 1)
	presentList := []*UserPresent{}
	query := `
	SELECT * FROM user_presents
	WHERE user_id = ? AND deleted_at IS NULL
	ORDER BY created_at DESC, id
	LIMIT ? OFFSET ?`
	if err = h.DB.Select(&presentList, query, userID, PresentCountPerPage, offset); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	var presentCount int
	if err = h.DB.Get(&presentCount, "SELECT COUNT(*) FROM user_presents WHERE user_id = ? AND deleted_at IS NULL", userID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	isNext := false
	if presentCount > (offset + PresentCountPerPage) {
		isNext = true
	}

	return successResponse(c, &ListPresentResponse{
		Presents: presentList,
		IsNext:   isNext,
	})
}

type ListPresentResponse struct {
	Presents []*UserPresent `json:"presents"`
	IsNext   bool           `json:"isNext"`
}

// receivePresent プレゼント受け取り
// POST /user/{userID}/present/receive
func (h *Handler) receivePresent(c echo.Context) error {
	// read body
	defer c.Request().Body.Close()
	req := new(ReceivePresentRequest)
	if err := parseRequestBody(c, req); err != nil {
		return errorResponse(c, http.StatusBadRequest, err)
	}

	userID, err := getUserID(c)
	if err != nil {
		return errorResponse(c, http.StatusBadRequest, err)
	}

	requestAt, err := getRequestTime(c)
	if err != nil {
		return errorResponse(c, http.StatusInternalServerError, ErrGetRequestTime)
	}

	if len(req.PresentIDs) == 0 {
		return errorResponse(c, http.StatusUnprocessableEntity, fmt.Errorf("presentIds is empty"))
	}

	if err = h.checkViewerID(userID, req.ViewerID); err != nil {
		if err == ErrUserDeviceNotFound {
			return errorResponse(c, http.StatusNotFound, err)
		}
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	// user_presentsに入っているが未取得のプレゼント取得
	query := "SELECT * FROM user_presents WHERE id IN (?) AND deleted_at IS NULL"
	query, params, err := sqlx.In(query, req.PresentIDs)
	if err != nil {
		return errorResponse(c, http.StatusBadRequest, err)
	}
	obtainPresent := []*UserPresent{}
	if err = h.DB.Select(&obtainPresent, query, params...); err != nil {
		return errorResponse(c, http.StatusBadRequest, err)
	}

	if len(obtainPresent) == 0 {
		return successResponse(c, &ReceivePresentResponse{
			UpdatedResources: makeUpdatedResources(requestAt, nil, nil, nil, nil, nil, nil, []*UserPresent{}),
		})
	}

	tx, err := h.DB.Beginx()
	if err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}
	defer tx.Rollback() //nolint:errcheck

	// 配布処理
	for i := range obtainPresent {
		if obtainPresent[i].DeletedAt != nil {
			return errorResponse(c, http.StatusInternalServerError, fmt.Errorf("received present"))
		}

		obtainPresent[i].UpdatedAt = requestAt
		obtainPresent[i].DeletedAt = &requestAt
		v := obtainPresent[i]
		// query = "DELETE FROM user_presents WHERE id=?"
		// _, err := tx.Exec(query, obtainPresent[i].ID)
		query = "UPDATE user_presents SET deleted_at=?, updated_at=? WHERE id=?"
		_, err := tx.Exec(query, requestAt, requestAt, v.ID)
		if err != nil {
			return errorResponse(c, http.StatusInternalServerError, err)
		}
	}

	_, _, _, err = h.obtainItem(tx, obtainPresent, requestAt)

	if err != nil {
		if err == ErrUserNotFound || err == ErrItemNotFound {
			return errorResponse(c, http.StatusNotFound, err)
		}
		if err == ErrInvalidItemType {
			return errorResponse(c, http.StatusBadRequest, err)
		}
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	err = tx.Commit()
	if err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	return successResponse(c, &ReceivePresentResponse{
		UpdatedResources: makeUpdatedResources(requestAt, nil, nil, nil, nil, nil, nil, obtainPresent),
	})
}

// obtainItem アイテム付与処理
func (h *Handler) obtainItem(tx *sqlx.Tx, obtainPresents []*UserPresent, requestAt int64) ([]int64, []*UserCard, []*UserItem, error) {
	obtainCoins := map[int64]*UserPresent{}
	obtainCards := map[int64]*UserPresent{}
	obtainMaterials := map[int64]*UserPresent{}
	obtainCoinsId := make([]int64, 0)
	obtainCardsId := make([]int64, 0)
	obtainMaterialsId := make([]int64, 0)

	for _, obtainPresent := range obtainPresents {
		switch obtainPresent.ItemType {
		case 1: // coin
			obtainCoins[obtainPresent.UserID] = obtainPresent
			obtainCoinsId = append(obtainCoinsId, obtainPresent.UserID)
		case 2: // card(ハンマー)
			obtainCards[obtainPresent.UserID] = obtainPresent
			obtainCardsId = append(obtainCardsId, obtainPresent.UserID)
		case 3, 4: // 強化素材
			obtainMaterials[obtainPresent.UserID] = obtainPresent
			obtainMaterialsId = append(obtainMaterialsId, obtainPresent.UserID)
		default:
			return nil, nil, nil, ErrInvalidItemType
		}
	}

	err := h.obtainCoin(tx, obtainCoins, obtainCoinsId, requestAt)
	if err != nil {
		return nil, nil, nil, err
	}
	err = h.obtainCard(tx, obtainCards, obtainCardsId, requestAt)
	if err != nil {
		return nil, nil, nil, err
	}
	err = h.obtainMaterial(tx, obtainMaterials, obtainMaterialsId, requestAt)
	if err != nil {
		return nil, nil, nil, err
	}

	return nil, nil, nil, nil
}

func (h *Handler) obtainCoin(tx *sqlx.Tx, obtainCoins map[int64]*UserPresent, obtainCoinsId []int64, requestAt int64) error {
	for _, obtainCoin := range obtainCoins {
		obtainAmount := obtainCoin.Amount
		userID := obtainCoin.UserID

		user := new(User)
		query := "SELECT * FROM users WHERE id=?"
		if err := tx.Get(user, query, userID); err != nil {
			if err == sql.ErrNoRows {
				return ErrUserNotFound
			}
			return err
		}

		query = "UPDATE users SET isu_coin=? WHERE id=?"
		totalCoin := user.IsuCoin + int64(obtainAmount)
		if _, err := tx.Exec(query, totalCoin, user.ID); err != nil {
			return err
		}
	}
	// query := "SELECT * FROM users WHERE id IN (?)"
	// query, params, err := sqlx.In(query, obtainCoinsId)
	// if err != nil {
	// 	return err
	// }
	// users := make([]*User, 0)
	// if err := tx.Select(&users, query, params...); err != nil {
	// 	return err
	// }

	// type BulkUpdateIsuCoin struct {
	// 	UserID    int64 `db:"id"`
	// 	TotalCoin int64 `db:"isu_coin"`
	// }
	// totalCoins := []BulkUpdateIsuCoin{}
	// for _, user := range users {
	// 	totalCoins = append(totalCoins, BulkUpdateIsuCoin{
	// 		UserID:    user.ID,
	// 		TotalCoin: user.IsuCoin + int64(obtainCoins[user.ID].Amount),
	// 	})
	// }
	// query = "INSERT INTO users (`id`, `isu_coin`) VALUES (:id, :isu_coin) ON DUPLICATE KEY UPDATE `isu_coin` = VALUES(`isu_coin`)"
	// if _, err := tx.NamedExec(query, totalCoins); err != nil {
	// 	return err
	// }

	return nil
}

func (h *Handler) obtainCard(tx *sqlx.Tx, obtainCards map[int64]*UserPresent, obtainCardsId []int64, requestAt int64) error {
	for _, obtainCard := range obtainCards {
		userID := obtainCard.UserID
		itemID := obtainCard.ItemID
		itemType := obtainCard.ItemType

		query := "SELECT * FROM item_masters WHERE id=? AND item_type=?"
		item := new(ItemMaster)
		if err := tx.Get(item, query, itemID, itemType); err != nil {
			if err == sql.ErrNoRows {
				return ErrItemNotFound
			}
			return err
		}

		cID, err := h.generateID()
		if err != nil {
			return err
		}
		card := &UserCard{
			ID:           cID,
			UserID:       userID,
			CardID:       item.ID,
			AmountPerSec: *item.AmountPerSec,
			Level:        1,
			TotalExp:     0,
			CreatedAt:    requestAt,
			UpdatedAt:    requestAt,
		}
		query = "INSERT INTO user_cards(id, user_id, card_id, amount_per_sec, level, total_exp, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"
		if _, err := tx.Exec(query, card.ID, card.UserID, card.CardID, card.AmountPerSec, card.Level, card.TotalExp, card.CreatedAt, card.UpdatedAt); err != nil {
			return err
		}
	}

	return nil
}

func (h *Handler) obtainMaterial(tx *sqlx.Tx, obtainMaterials map[int64]*UserPresent, obtainMaterialsId []int64, requestAt int64) error {
	for _, obtainMaterial := range obtainMaterials {
		userID := obtainMaterial.UserID
		obtainAmount := obtainMaterial.Amount
		itemID := obtainMaterial.ItemID
		itemType := obtainMaterial.ItemType

		query := "SELECT * FROM item_masters WHERE id=? AND item_type=?"
		item := new(ItemMaster)
		if err := tx.Get(item, query, itemID, itemType); err != nil {
			if err == sql.ErrNoRows {
				return ErrItemNotFound
			}
			return err
		}
		// 所持数取得
		query = "SELECT * FROM user_items WHERE user_id=? AND item_id=?"
		uitem := new(UserItem)
		if err := tx.Get(uitem, query, userID, item.ID); err != nil {
			if err != sql.ErrNoRows {
				return err
			}
			uitem = nil
		}

		if uitem == nil { // 新規作成
			uitemID, err := h.generateID()
			if err != nil {
				return err
			}
			uitem = &UserItem{
				ID:        uitemID,
				UserID:    userID,
				ItemType:  item.ItemType,
				ItemID:    item.ID,
				Amount:    int(obtainAmount),
				CreatedAt: requestAt,
				UpdatedAt: requestAt,
			}
			query = "INSERT INTO user_items(id, user_id, item_id, item_type, amount, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
			if _, err := tx.Exec(query, uitem.ID, userID, uitem.ItemID, uitem.ItemType, uitem.Amount, requestAt, requestAt); err != nil {
				return err
			}

		} else { // 更新
			uitem.Amount += int(obtainAmount)
			uitem.UpdatedAt = requestAt
			query = "UPDATE user_items SET amount=?, updated_at=? WHERE id=?"
			if _, err := tx.Exec(query, uitem.Amount, uitem.UpdatedAt, uitem.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

type ReceivePresentRequest struct {
	ViewerID   string  `json:"viewerId"`
	PresentIDs []int64 `json:"presentIds"`
}

type ReceivePresentResponse struct {
	UpdatedResources *UpdatedResource `json:"updatedResources"`
}

// listItem アイテムリスト
// GET /user/{userID}/item
func (h *Handler) listItem(c echo.Context) error {
	userID, err := getUserID(c)
	if err != nil {
		return errorResponse(c, http.StatusBadRequest, err)
	}

	requestAt, err := getRequestTime(c)
	if err != nil {
		return errorResponse(c, http.StatusInternalServerError, ErrGetRequestTime)
	}

	user := new(User)
	query := "SELECT * FROM users WHERE id=?"
	if err = h.DB.Get(user, query, userID); err != nil {
		if err == sql.ErrNoRows {
			return errorResponse(c, http.StatusNotFound, ErrUserNotFound)
		}
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	itemList := []*UserItem{}
	query = "SELECT * FROM user_items WHERE user_id = ?"
	if err = h.DB.Select(&itemList, query, userID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	cardList := make([]*UserCard, 0)
	query = "SELECT * FROM user_cards WHERE user_id=?"
	if err = h.DB.Select(&cardList, query, userID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	// genearte one time token
	query = "UPDATE user_one_time_tokens SET deleted_at=? WHERE user_id=? AND deleted_at IS NULL"
	if _, err = h.DB.Exec(query, requestAt, userID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}
	tID, err := h.generateID()
	if err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}
	tk, err := generateULID()
	if err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}
	token := &UserOneTimeToken{
		ID:        tID,
		UserID:    userID,
		Token:     tk,
		TokenType: 2,
		CreatedAt: requestAt,
		UpdatedAt: requestAt,
		ExpiredAt: requestAt + 600,
	}
	query = "INSERT INTO user_one_time_tokens(id, user_id, token, token_type, created_at, updated_at, expired_at) VALUES (?, ?, ?, ?, ?, ?, ?)"
	if _, err = h.DB.Exec(query, token.ID, token.UserID, token.Token, token.TokenType, token.CreatedAt, token.UpdatedAt, token.ExpiredAt); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	return successResponse(c, &ListItemResponse{
		OneTimeToken: token.Token,
		Items:        itemList,
		User:         user,
		Cards:        cardList,
	})
}

type ListItemResponse struct {
	OneTimeToken string      `json:"oneTimeToken"`
	User         *User       `json:"user"`
	Items        []*UserItem `json:"items"`
	Cards        []*UserCard `json:"cards"`
}
