package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"
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
	if err = c.Get("db").(*sqlx.DB).Select(&presentList, query, userID, PresentCountPerPage, offset); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	var presentCount int
	if err = c.Get("db").(*sqlx.DB).Get(&presentCount, "SELECT COUNT(*) FROM user_presents WHERE user_id = ? AND deleted_at IS NULL", userID); err != nil {
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

	if err = h.checkViewerID(c, userID, req.ViewerID); err != nil {
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
	if err = c.Get("db").(*sqlx.DB).Select(&obtainPresent, query, params...); err != nil {
		return errorResponse(c, http.StatusBadRequest, err)
	}

	if len(obtainPresent) == 0 {
		return successResponse(c, &ReceivePresentResponse{
			UpdatedResources: makeUpdatedResources(requestAt, nil, nil, nil, nil, nil, nil, []*UserPresent{}),
		})
	}

	tx, err := c.Get("db").(*sqlx.DB).Beginx()
	if err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}
	defer tx.Rollback() //nolint:errcheck

	obtainCoins := []*UserPresent{}
	obtainCards := []*UserPresent{}
	obtainGems := []*UserPresent{}
	// 配布処理
	for i := range obtainPresent {
		if obtainPresent[i].DeletedAt != nil {
			return errorResponse(c, http.StatusInternalServerError, fmt.Errorf("received present"))
		}

		obtainPresent[i].UpdatedAt = requestAt
		obtainPresent[i].DeletedAt = &requestAt

		switch obtainPresent[i].ItemType {
		case 1: // coin
			obtainCoins = append(obtainCoins, obtainPresent[i])
		case 2: // card(ハンマー)
			obtainCards = append(obtainCards, obtainPresent[i])
		case 3, 4: // 強化素材
			obtainGems = append(obtainGems, obtainPresent[i])
		default:
			return errorResponse(c, http.StatusBadRequest, err)
		}
	}

	eg := errgroup.Group{}

	eg.Go(func() error {
		tx1, err := c.Get("db").(*sqlx.DB).Beginx()
		if err != nil {
			return errorResponse(c, http.StatusInternalServerError, err)
		}
		defer tx1.Rollback() //nolint:errcheck

		err = h.obtainCoins(tx1, obtainCoins)
		if err != nil {
			if err == ErrUserNotFound || err == ErrItemNotFound {
				return errorResponse(c, http.StatusNotFound, err)
			}
			return errorResponse(c, http.StatusInternalServerError, err)
		}

		err = tx1.Commit()
		if err != nil {
			return errorResponse(c, http.StatusInternalServerError, err)
		}
		return nil
	})

	eg.Go(func() error {
		tx2, err := c.Get("db").(*sqlx.DB).Beginx()
		if err != nil {
			return errorResponse(c, http.StatusInternalServerError, err)
		}
		defer tx2.Rollback() //nolint:errcheck

		err = h.obtainCards(tx2, obtainCards)
		if err != nil {
			if err == ErrUserNotFound || err == ErrItemNotFound {
				return errorResponse(c, http.StatusNotFound, err)
			}
			return errorResponse(c, http.StatusInternalServerError, err)
		}

		err = tx2.Commit()
		if err != nil {
			return errorResponse(c, http.StatusInternalServerError, err)
		}
		return nil
	})

	eg.Go(func() error {
		tx3, err := c.Get("db").(*sqlx.DB).Beginx()
		if err != nil {
			return errorResponse(c, http.StatusInternalServerError, err)
		}
		defer tx3.Rollback() //nolint:errcheck

		err = h.obtainGems(tx3, obtainGems)
		if err != nil {
			if err == ErrUserNotFound || err == ErrItemNotFound {
				return errorResponse(c, http.StatusNotFound, err)
			}
			return errorResponse(c, http.StatusInternalServerError, err)
		}

		err = tx3.Commit()
		if err != nil {
			return errorResponse(c, http.StatusInternalServerError, err)
		}
		return nil
	})

	eg.Go(func() error {
		tx4, err := c.Get("db").(*sqlx.DB).Beginx()
		if err != nil {
			return errorResponse(c, http.StatusInternalServerError, err)
		}
		defer tx4.Rollback() //nolint:errcheck

		query = "INSERT INTO user_presents(id, user_id, sent_at, item_type, item_id, amount, present_message, created_at, deleted_at, updated_at)" +
			" VALUES (:id, :user_id, :sent_at, :item_type, :item_id, :amount, :present_message, :created_at, :deleted_at, :updated_at)" +
			" ON DUPLICATE KEY UPDATE deleted_at=VALUES(deleted_at), updated_at=VALUES(updated_at)"
		_, err = tx4.NamedExec(query, obtainPresent)
		if err != nil {
			return errorResponse(c, http.StatusInternalServerError, err)
		}

		err = tx4.Commit()
		if err != nil {
			return errorResponse(c, http.StatusInternalServerError, err)
		}
		return nil
	})

	err = tx.Commit()
	if err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	err = eg.Wait()
	if err != nil {
		return nil
	}

	return successResponse(c, &ReceivePresentResponse{
		UpdatedResources: makeUpdatedResources(requestAt, nil, nil, nil, nil, nil, nil, obtainPresent),
	})
}

type ReceivePresentRequest struct {
	ViewerID   string  `json:"viewerId"`
	PresentIDs []int64 `json:"presentIds"`
}

type ReceivePresentResponse struct {
	UpdatedResources *UpdatedResource `json:"updatedResources"`
}

func (h *Handler) obtainCoins(tx *sqlx.Tx, obtainCoins []*UserPresent) error {
	userCoinIds := make([]int64, len(obtainCoins))
	userCoinFromDB := make([]*User, len(obtainCoins))
	userCoinMap := make(map[int64]*User, len(obtainCoins))
	userCoins := make([]*User, 0, len(obtainCoins))

	if len(obtainCoins) == 0 {
		return nil
	}

	for i := range obtainCoins {
		userCoinIds = append(userCoinIds, obtainCoins[i].UserID)
	}

	query := "SELECT * FROM users WHERE id IN (?)"
	query, params, err := sqlx.In(query, userCoinIds)
	if err != nil {
		return err
	}
	err = tx.Select(&userCoinFromDB, query, params...)
	if err != nil {
		return err
	}
	for i := range userCoinFromDB {
		userCoinMap[userCoinFromDB[i].ID] = userCoinFromDB[i]
	}
	for i := range obtainCoins {
		userCoinMap[obtainCoins[i].UserID].IsuCoin += int64(obtainCoins[i].Amount)
		userCoins = append(userCoins, userCoinMap[obtainCoins[i].UserID])
	}

	if len(userCoins) <= 0 {
		return nil
	}

	// query := "UPDATE users SET isu_coin=? WHERE id=?"
	query = "INSERT INTO users(id, isu_coin, last_activated_at, registered_at, last_getreward_at, created_at, updated_at)" +
		" VALUES(:id, :isu_coin, :last_activated_at, :registered_at, :last_getreward_at, :created_at, :updated_at)" +
		" ON DUPLICATE KEY UPDATE isu_coin = VALUES(isu_coin)"
	if _, err := tx.NamedExec(query, userCoins); err != nil {
		return err
	}

	return nil
}

func (h *Handler) obtainCards(tx *sqlx.Tx, obtainCards []*UserPresent) error {
	query := "SELECT * FROM item_masters WHERE item_type=?"
	items := []*ItemMaster{}
	if err := tx.Select(&items, query, 2); err != nil {
		return err
	}

	cards := make([]*UserCard, 0, len(obtainCards))

	for i := range obtainCards {
		var item *ItemMaster
		for j := range items {
			if obtainCards[i].ItemID == items[j].ID {
				item = items[j]
			}
		}
		if item == nil {
			return ErrItemNotFound
		}

		cID, err := h.generateID()
		if err != nil {
			return err
		}
		card := &UserCard{
			ID:           cID,
			UserID:       obtainCards[i].UserID,
			CardID:       item.ID,
			AmountPerSec: *item.AmountPerSec,
			Level:        1,
			TotalExp:     0,
			CreatedAt:    obtainCards[i].UpdatedAt,
			UpdatedAt:    obtainCards[i].UpdatedAt,
		}

		cards = append(cards, card)
	}

	if len(cards) <= 0 {
		return nil
	}

	query = "INSERT INTO user_cards(id, user_id, card_id, amount_per_sec, level, total_exp, created_at, updated_at)" +
		" VALUES (:id, :user_id, :card_id, :amount_per_sec, :level, :total_exp, :created_at, :updated_at)"
	if _, err := tx.NamedExec(query, cards); err != nil {
		return err
	}

	return nil
}

func (h *Handler) obtainGems(tx *sqlx.Tx, obtainGems []*UserPresent) error {
	query := "SELECT * FROM item_masters WHERE id IN (?)"
	items := []*ItemMaster{}
	if err := tx.Select(&items, query); err != nil {
		return err
	}

	uItemsMap := make(map[string]*UserItem, len(obtainGems))

	for i := range obtainGems {
		var item *ItemMaster
		for j := range items {
			if obtainGems[i].ItemID == items[j].ID {
				item = items[j]
			}
		}
		if item == nil {
			return ErrItemNotFound
		}

		// 所持数取得
		index := strconv.Itoa(int(obtainGems[i].UserID)) + strconv.Itoa(int(item.ID))
		_, ok := uItemsMap[index]
		if !ok {
			query = "SELECT * FROM user_items WHERE user_id=? AND item_id=?"
			uItemsMap[index] = new(UserItem)
			if err := tx.Get(uItemsMap[index], query, obtainGems[i].UserID, item.ID); err != nil {
				if err != sql.ErrNoRows {
					return err
				}
				uItemsMap[index] = nil
			}
		}

		if uItemsMap[index] == nil { // 新規作成
			uItemID, err := h.generateID()
			if err != nil {
				return err
			}
			uItemsMap[index] = &UserItem{
				ID:        uItemID,
				UserID:    obtainGems[i].UserID,
				ItemType:  item.ItemType,
				ItemID:    item.ID,
				Amount:    obtainGems[i].Amount,
				CreatedAt: obtainGems[i].UpdatedAt,
				UpdatedAt: obtainGems[i].UpdatedAt,
			}
		} else { // 更新
			uItemsMap[index].Amount += int(obtainGems[i].Amount)
			uItemsMap[index].UpdatedAt = obtainGems[i].UpdatedAt
		}
	}

	uItems := make([]*UserItem, 0, len(uItemsMap))
	for _, v := range uItemsMap {
		if v != nil {
			uItems = append(uItems, v)
		}
	}

	if len(uItems) <= 0 {
		return nil
	}

	query = "INSERT INTO user_items(id, user_id, item_id, item_type, amount, created_at, updated_at)" +
		" VALUES (:id, :user_id, :item_id, :item_type, :amount, :created_at, :updated_at)" +
		" ON DUPLICATE KEY UPDATE amount = VALUES(amount), updated_at=VALUES(updated_at)"
	if _, err := tx.NamedExec(query, uItems); err != nil {
		return err
	}

	return nil
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
	if err = c.Get("db").(*sqlx.DB).Get(user, query, userID); err != nil {
		if err == sql.ErrNoRows {
			return errorResponse(c, http.StatusNotFound, ErrUserNotFound)
		}
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	itemList := []*UserItem{}
	query = "SELECT * FROM user_items WHERE user_id = ?"
	if err = c.Get("db").(*sqlx.DB).Select(&itemList, query, userID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	cardList := make([]*UserCard, 0)
	query = "SELECT * FROM user_cards WHERE user_id=?"
	if err = c.Get("db").(*sqlx.DB).Select(&cardList, query, userID); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	// generate one time token
	query = "UPDATE user_one_time_tokens SET deleted_at=? WHERE user_id=? AND deleted_at IS NULL"
	if _, err = c.Get("db").(*sqlx.DB).Exec(query, requestAt, userID); err != nil {
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
	if _, err = c.Get("db").(*sqlx.DB).Exec(query, token.ID, token.UserID, token.Token, token.TokenType, token.CreatedAt, token.UpdatedAt, token.ExpiredAt); err != nil {
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
