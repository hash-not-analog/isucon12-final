package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/logica0419/helpisu"
	"github.com/pkg/errors"
)

var (
	ErrInvalidRequestBody       error = fmt.Errorf("invalid request body")
	ErrInvalidMasterVersion     error = fmt.Errorf("invalid master version")
	ErrInvalidItemType          error = fmt.Errorf("invalid item type")
	ErrInvalidToken             error = fmt.Errorf("invalid token")
	ErrGetRequestTime           error = fmt.Errorf("failed to get request time")
	ErrExpiredSession           error = fmt.Errorf("session expired")
	ErrUserNotFound             error = fmt.Errorf("not found user")
	ErrUserDeviceNotFound       error = fmt.Errorf("not found user device")
	ErrItemNotFound             error = fmt.Errorf("not found item")
	ErrLoginBonusRewardNotFound error = fmt.Errorf("not found login bonus reward")
	ErrNoFormFile               error = fmt.Errorf("no such file")
	ErrUnauthorized             error = fmt.Errorf("unauthorized user")
	ErrForbidden                error = fmt.Errorf("forbidden")
	ErrGeneratePassword         error = fmt.Errorf("failed to password hash") //nolint:deadcode
)

const (
	DeckCardNumber      int = 3
	PresentCountPerPage int = 100

	SQLDirectory string = "../sql/"
)

type Handler struct {
	DB  *sqlx.DB
	DB2 *sqlx.DB
	DB3 *sqlx.DB
}

var d = helpisu.NewDBDisconnectDetector(5, 80)

func main() {
	http.DefaultTransport.(*http.Transport).MaxIdleConns = 0           // default: 100
	http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = 1024 // default: 2

	rand.Seed(time.Now().UnixNano())
	time.Local = time.FixedZone("Local", 9*60*60)

	e := echo.New()
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost},
		AllowHeaders: []string{"Content-Type", "x-master-version", "x-session"},
	}))

	e.JSONSerializer = helpisu.NewSonicSerializer()

	// connect db
	dbx, err := connectDB(false, 1)
	if err != nil {
		e.Logger.Fatalf("failed to connect to db: %v", err)
	}
	defer dbx.Close()

	dbx2, err := connectDB(false, 2)
	if err != nil {
		e.Logger.Fatalf("failed to connect to db: %v", err)
	}
	defer dbx.Close()

	dbx3, err := connectDB(false, 3)
	if err != nil {
		e.Logger.Fatalf("failed to connect to db: %v", err)
	}
	defer dbx.Close()

	d.RegisterDB(dbx.DB)
	go d.Start()

	// setting server
	e.Server.Addr = fmt.Sprintf(":%v", "8080")
	h := &Handler{
		DB:  dbx,
		DB2: dbx2,
		DB3: dbx3,
	}

	// e.Use(middleware.CORS())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{}))

	// utility
	e.POST("/initialize", initialize)
	e.GET("/health", h.health)
	e.GET("/admin/generate", h.GenerateID)

	// feature
	API := e.Group("", h.apiMiddleware)
	API.POST("/user", h.createUser)
	API.POST("/login", h.login)
	sessCheckAPI := API.Group("", h.checkSessionMiddleware)
	sessCheckAPI.GET("/user/:userID/gacha/index", h.listGacha)
	sessCheckAPI.POST("/user/:userID/gacha/draw/:gachaID/:n", h.drawGacha)
	sessCheckAPI.GET("/user/:userID/present/index/:n", h.listPresent)
	sessCheckAPI.POST("/user/:userID/present/receive", h.receivePresent)
	sessCheckAPI.GET("/user/:userID/item", h.listItem)
	sessCheckAPI.POST("/user/:userID/card/addexp/:cardID", h.addExpToCard)
	sessCheckAPI.POST("/user/:userID/card", h.updateDeck)
	sessCheckAPI.POST("/user/:userID/reward", h.reward)
	sessCheckAPI.GET("/user/:userID/home", h.home)

	// admin
	adminAPI := e.Group("", h.adminMiddleware)
	adminAPI.POST("/admin/login", h.adminLogin)
	adminAuthAPI := adminAPI.Group("", h.adminSessionCheckMiddleware)
	adminAuthAPI.DELETE("/admin/logout", h.adminLogout)
	adminAuthAPI.GET("/admin/master", h.adminListMaster)
	adminAuthAPI.PUT("/admin/master", h.adminUpdateMaster)
	adminAuthAPI.GET("/admin/user/:userID", h.adminUser)
	adminAuthAPI.POST("/admin/user/:userID/ban", h.adminBanUser)

	e.Logger.Infof("Start server: address=%s", e.Server.Addr)
	e.Logger.Error(e.StartServer(e.Server))
}

func connectDB(batch bool, index int) (*sqlx.DB, error) {
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=%s&multiStatements=%t&interpolateParams=true",
		getEnv("ISUCON_DB_USER", "isucon"),
		getEnv("ISUCON_DB_PASSWORD", "isucon"),
		getEnv(fmt.Sprintf("ISUCON_DB_HOST_%d", index), "127.0.0.1"),
		getEnv("ISUCON_DB_PORT", "3306"),
		getEnv("ISUCON_DB_NAME", "isucon"),
		"Asia%2FTokyo",
		batch,
	)
	dbx, err := sqlx.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	// プール内に保持できるアイドル接続数の制限を設定 (default: 2)
	dbx.SetMaxIdleConns(1024)
	// 接続してから再利用できる最大期間
	dbx.SetConnMaxLifetime(0)
	// アイドル接続してから再利用できる最大期間
	dbx.SetConnMaxIdleTime(0)

	return dbx, nil
}

// adminMiddleware
func (h *Handler) adminMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		requestAt := time.Now()
		c.Set("requestTime", requestAt.Unix())

		// next
		if err := next(c); err != nil {
			c.Error(err)
		}
		return nil
	}
}

// apiMiddleware
func (h *Handler) apiMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		requestAt, err := time.Parse(time.RFC1123, c.Request().Header.Get("x-isu-date"))
		if err != nil {
			requestAt = time.Now()
		}
		c.Set("requestTime", requestAt.Unix())

		// マスタ確認
		query := "SELECT * FROM version_masters WHERE status=1"
		masterVersion := new(VersionMaster)
		if err := h.DB.Get(masterVersion, query); err != nil {
			if err == sql.ErrNoRows {
				return errorResponse(c, http.StatusNotFound, fmt.Errorf("active master version is not found"))
			}
			return errorResponse(c, http.StatusInternalServerError, err)
		}

		if masterVersion.MasterVersion != c.Request().Header.Get("x-master-version") {
			return errorResponse(c, http.StatusUnprocessableEntity, ErrInvalidMasterVersion)
		}

		// check ban
		userID, err := getUserID(c)
		if err == nil && userID != 0 {
			isBan, err := h.checkBan(userID)
			if err != nil {
				return errorResponse(c, http.StatusInternalServerError, err)
			}
			if isBan {
				return errorResponse(c, http.StatusForbidden, ErrForbidden)
			}
		}

		// next
		if err := next(c); err != nil {
			c.Error(err)
		}
		return nil
	}
}

// checkSessionMiddleware
func (h *Handler) checkSessionMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		sessID := c.Request().Header.Get("x-session")
		if sessID == "" {
			return errorResponse(c, http.StatusUnauthorized, ErrUnauthorized)
		}

		userID, err := getUserID(c)
		if err != nil {
			return errorResponse(c, http.StatusBadRequest, err)
		}

		requestAt, err := getRequestTime(c)
		if err != nil {
			return errorResponse(c, http.StatusInternalServerError, ErrGetRequestTime)
		}

		userSession := new(Session)
		query := "SELECT * FROM user_sessions WHERE session_id=? AND expired_at > ?"
		if err := h.DB.Get(userSession, query, sessID, requestAt); err != nil {
			if err == sql.ErrNoRows {
				return errorResponse(c, http.StatusUnauthorized, ErrUnauthorized)
			}
			return errorResponse(c, http.StatusInternalServerError, err)
		}

		if userSession.UserID != userID {
			return errorResponse(c, http.StatusForbidden, ErrForbidden)
		}

		// if userSession.ExpiredAt < requestAt {
		// 	query = "DELETE FROM user_sessions WHERE session_id=?"
		// 	if _, err = h.DB.Exec(query, sessID); err != nil {
		// 		return errorResponse(c, http.StatusInternalServerError, err)
		// 	}
		// 	return errorResponse(c, http.StatusUnauthorized, ErrExpiredSession)
		// }

		// next
		if err := next(c); err != nil {
			c.Error(err)
		}
		return nil
	}
}

// checkOneTimeToken
func (h *Handler) checkOneTimeToken(token string, tokenType int, requestAt int64) error {
	tk := new(UserOneTimeToken)
	query := "SELECT * FROM user_one_time_tokens WHERE token=? AND token_type=? AND deleted_at IS NULL"
	if err := h.DB.Get(tk, query, token, tokenType); err != nil {
		if err == sql.ErrNoRows {
			return ErrInvalidToken
		}
		return err
	}

	if tk.ExpiredAt < requestAt {
		query = "UPDATE user_one_time_tokens SET deleted_at=? WHERE token=?"
		if _, err := h.DB.Exec(query, requestAt, token); err != nil {
			return err
		}
		return ErrInvalidToken
	}

	// 使ったトークンを失効する
	query = "UPDATE user_one_time_tokens SET deleted_at=? WHERE token=?"
	if _, err := h.DB.Exec(query, requestAt, token); err != nil {
		return err
	}

	return nil
}

// checkViewerID
func (h *Handler) checkViewerID(userID int64, viewerID string) error {
	query := "SELECT * FROM user_devices WHERE user_id=? AND platform_id=?"
	device := new(UserDevice)
	if err := h.DB.Get(device, query, userID, viewerID); err != nil {
		if err == sql.ErrNoRows {
			return ErrUserDeviceNotFound
		}
		return err
	}

	return nil
}

// checkBan
func (h *Handler) checkBan(userID int64) (bool, error) {
	_, ok := userBanCache.Get(userID)
	if !ok {
		return false, nil
	}
	return true, nil
}

// getRequestTime リクエストを受けた時間をコンテキストからunixtimeで取得する
func getRequestTime(c echo.Context) (int64, error) {
	v := c.Get("requestTime")
	if requestTime, ok := v.(int64); ok {
		return requestTime, nil
	}
	return 0, ErrGetRequestTime
}

// initialize 初期化処理
// POST /initialize
func initialize(c echo.Context) error {
	helpisu.ResetAllCache()

	wg := sync.WaitGroup{}
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			dbx, err := connectDB(true, index)
			if err != nil {
				return
			}
			defer dbx.Close()

			out, err := exec.Command("/bin/sh", "-c", SQLDirectory+fmt.Sprintf("init%d.sh", index)).CombinedOutput()
			if err != nil {
				c.Logger().Errorf("Failed to initialize %s: %v", string(out), err)
				return
			}

			var banUsers []*UserBan
			query := "SELECT * FROM user_bans"
			if err := dbx.Select(&banUsers, query); err != nil {
				c.Logger().Errorf("Failed to initialize: %v", err)
				return
			}
			for _, banUser := range banUsers {
				userBanCache.Set(banUser.UserID, *banUser)
			}
		}(i)
	}

	wg.Wait()

	d.Pause()

	return successResponse(c, &InitializeResponse{
		Language: "go",
	})
}

type InitializeResponse struct {
	Language string `json:"language"`
}

// //////////////////////////////////////
// util

// health ヘルスチェック
func (h *Handler) health(c echo.Context) error {
	return c.String(http.StatusOK, "OK")
}

// errorResponse returns error.
func errorResponse(c echo.Context, statusCode int, err error) error {
	c.Logger().Errorf("status=%d, err=%+v", statusCode, errors.WithStack(err))

	return c.JSON(statusCode, struct {
		StatusCode int    `json:"status_code"`
		Message    string `json:"message"`
	}{
		StatusCode: statusCode,
		Message:    err.Error(),
	})
}

// successResponse responds success.
func successResponse(c echo.Context, v interface{}) error {
	return c.JSON(http.StatusOK, v)
}

// noContentResponse
func noContentResponse(c echo.Context, status int) error {
	return c.NoContent(status)
}

func (h *Handler) GenerateID(c echo.Context) error {
	id, _ := h.generateID()
	return c.Blob(http.StatusOK, "text/plain", []byte(strconv.Itoa(int(id))))
}

var duplicatedIDMap = helpisu.NewCache[int, struct{}]()

// generateID uniqueなIDを生成する
func (h *Handler) generateID() (int64, error) {
	if root := os.Getenv("id_root"); root != "" {
		resp, err := http.Get(fmt.Sprintf("http://%s:8080/admin/generate", root))
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()

		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return 0, err
		}

		id, err := strconv.Atoi(string(b))
		if err != nil {
			return 0, nil
		}

		return int64(id), nil
	}

	var id int

	for {
		id = rand.Intn(8223372036854775807)
		_, ok := duplicatedIDMap.Get(id)
		if !ok {
			break
		}
	}
	duplicatedIDMap.Set(id, struct{}{})

	return int64(id + 100000000001), nil
}

// generateSessionID
func generateULID() (string, error) {
	id := helpisu.NewULID()

	return id, nil
}

// getUserID gets userID by path param.
func getUserID(c echo.Context) (int64, error) {
	return strconv.ParseInt(c.Param("userID"), 10, 64)
}

// getEnv gets environment variable.
func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v == "" {
		return defaultVal
	} else {
		return v
	}
}

// parseRequestBody parses request body.
func parseRequestBody(c echo.Context, dist interface{}) error {
	buf, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return ErrInvalidRequestBody
	}
	if err = json.Unmarshal(buf, &dist); err != nil {
		return ErrInvalidRequestBody
	}
	return nil
}

type UpdatedResource struct {
	Now  int64 `json:"now"`
	User *User `json:"user,omitempty"`

	UserDevice       *UserDevice       `json:"userDevice,omitempty"`
	UserCards        []*UserCard       `json:"userCards,omitempty"`
	UserDecks        []*UserDeck       `json:"userDecks,omitempty"`
	UserItems        []*UserItem       `json:"userItems,omitempty"`
	UserLoginBonuses []*UserLoginBonus `json:"userLoginBonuses,omitempty"`
	UserPresents     []*UserPresent    `json:"userPresents,omitempty"`
}

func makeUpdatedResources(
	requestAt int64,
	user *User,
	userDevice *UserDevice,
	userCards []*UserCard,
	userDecks []*UserDeck,
	userItems []*UserItem,
	userLoginBonuses []*UserLoginBonus,
	userPresents []*UserPresent,
) *UpdatedResource {
	return &UpdatedResource{
		Now:              requestAt,
		User:             user,
		UserDevice:       userDevice,
		UserCards:        userCards,
		UserItems:        userItems,
		UserDecks:        userDecks,
		UserLoginBonuses: userLoginBonuses,
		UserPresents:     userPresents,
	}
}

// //////////////////////////////////////
// entity

type User struct {
	ID              int64  `json:"id" db:"id"`
	IsuCoin         int64  `json:"isuCoin" db:"isu_coin"`
	LastGetRewardAt int64  `json:"lastGetRewardAt" db:"last_getreward_at"`
	LastActivatedAt int64  `json:"lastActivatedAt" db:"last_activated_at"`
	RegisteredAt    int64  `json:"registeredAt" db:"registered_at"`
	CreatedAt       int64  `json:"createdAt" db:"created_at"`
	UpdatedAt       int64  `json:"updatedAt" db:"updated_at"`
	DeletedAt       *int64 `json:"deletedAt,omitempty" db:"deleted_at"`
}

type UserDevice struct {
	ID           int64  `json:"id" db:"id"`
	UserID       int64  `json:"userId" db:"user_id"`
	PlatformID   string `json:"platformId" db:"platform_id"`
	PlatformType int    `json:"platformType" db:"platform_type"`
	CreatedAt    int64  `json:"createdAt" db:"created_at"`
	UpdatedAt    int64  `json:"updatedAt" db:"updated_at"`
	DeletedAt    *int64 `json:"deletedAt,omitempty" db:"deleted_at"`
}

type UserBan struct {
	ID        int64  `db:"id"`
	UserID    int64  `db:"user_id"`
	CreatedAt int64  `db:"created_at"`
	UpdatedAt int64  `db:"updated_at"`
	DeletedAt *int64 `db:"deleted_at"`
}

type UserCard struct {
	ID           int64  `json:"id" db:"id"`
	UserID       int64  `json:"userId" db:"user_id"`
	CardID       int64  `json:"cardId" db:"card_id"`
	AmountPerSec int    `json:"amountPerSec" db:"amount_per_sec"`
	Level        int    `json:"level" db:"level"`
	TotalExp     int64  `json:"totalExp" db:"total_exp"`
	CreatedAt    int64  `json:"createdAt" db:"created_at"`
	UpdatedAt    int64  `json:"updatedAt" db:"updated_at"`
	DeletedAt    *int64 `json:"deletedAt,omitempty" db:"deleted_at"`
}

type UserDeck struct {
	ID        int64  `json:"id" db:"id"`
	UserID    int64  `json:"userId" db:"user_id"`
	CardID1   int64  `json:"cardId1" db:"user_card_id_1"`
	CardID2   int64  `json:"cardId2" db:"user_card_id_2"`
	CardID3   int64  `json:"cardId3" db:"user_card_id_3"`
	CreatedAt int64  `json:"createdAt" db:"created_at"`
	UpdatedAt int64  `json:"updatedAt" db:"updated_at"`
	DeletedAt *int64 `json:"deletedAt,omitempty" db:"deleted_at"`
}

type UserItem struct {
	ID        int64  `json:"id" db:"id"`
	UserID    int64  `json:"userId" db:"user_id"`
	ItemType  int    `json:"itemType" db:"item_type"`
	ItemID    int64  `json:"itemId" db:"item_id"`
	Amount    int    `json:"amount" db:"amount"`
	CreatedAt int64  `json:"createdAt" db:"created_at"`
	UpdatedAt int64  `json:"updatedAt" db:"updated_at"`
	DeletedAt *int64 `json:"deletedAt,omitempty" db:"deleted_at"`
}

type UserLoginBonus struct {
	ID                 int64  `json:"id" db:"id"`
	UserID             int64  `json:"userId" db:"user_id"`
	LoginBonusID       int64  `json:"loginBonusId" db:"login_bonus_id"`
	LastRewardSequence int    `json:"lastRewardSequence" db:"last_reward_sequence"`
	LoopCount          int    `json:"loopCount" db:"loop_count"`
	CreatedAt          int64  `json:"createdAt" db:"created_at"`
	UpdatedAt          int64  `json:"updatedAt" db:"updated_at"`
	DeletedAt          *int64 `json:"deletedAt,omitempty" db:"deleted_at"`
}

type UserPresent struct {
	ID             int64  `json:"id" db:"id"`
	UserID         int64  `json:"userId" db:"user_id"`
	SentAt         int64  `json:"sentAt" db:"sent_at"`
	ItemType       int    `json:"itemType" db:"item_type"`
	ItemID         int64  `json:"itemId" db:"item_id"`
	Amount         int    `json:"amount" db:"amount"`
	PresentMessage string `json:"presentMessage" db:"present_message"`
	CreatedAt      int64  `json:"createdAt" db:"created_at"`
	UpdatedAt      int64  `json:"updatedAt" db:"updated_at"`
	DeletedAt      *int64 `json:"deletedAt,omitempty" db:"deleted_at"`
}

type UserPresentAllReceivedHistory struct {
	ID           int64  `json:"id" db:"id"`
	UserID       int64  `json:"userId" db:"user_id"`
	PresentAllID int64  `json:"presentAllId" db:"present_all_id"`
	ReceivedAt   int64  `json:"receivedAt" db:"received_at"`
	CreatedAt    int64  `json:"createdAt" db:"created_at"`
	UpdatedAt    int64  `json:"updatedAt" db:"updated_at"`
	DeletedAt    *int64 `json:"deletedAt,omitempty" db:"deleted_at"`
}

type Session struct {
	ID        int64  `json:"id" db:"id"`
	UserID    int64  `json:"userId" db:"user_id"`
	SessionID string `json:"sessionId" db:"session_id"`
	ExpiredAt int64  `json:"expiredAt" db:"expired_at"`
	CreatedAt int64  `json:"createdAt" db:"created_at"`
	UpdatedAt int64  `json:"updatedAt" db:"updated_at"`
	DeletedAt *int64 `json:"deletedAt,omitempty" db:"deleted_at"`
}

type UserOneTimeToken struct {
	ID        int64  `json:"id" db:"id"`
	UserID    int64  `json:"userId" db:"user_id"`
	Token     string `json:"token" db:"token"`
	TokenType int    `json:"tokenType" db:"token_type"`
	ExpiredAt int64  `json:"expiredAt" db:"expired_at"`
	CreatedAt int64  `json:"createdAt" db:"created_at"`
	UpdatedAt int64  `json:"updatedAt" db:"updated_at"`
	DeletedAt *int64 `json:"deletedAt,omitempty" db:"deleted_at"`
}

// //////////////////////////////////////
// master

type GachaMaster struct {
	ID           int64  `json:"id" db:"id"`
	Name         string `json:"name" db:"name"`
	StartAt      int64  `json:"startAt" db:"start_at"`
	EndAt        int64  `json:"endAt" db:"end_at"`
	DisplayOrder int    `json:"displayOrder" db:"display_order"`
	CreatedAt    int64  `json:"createdAt" db:"created_at"`
}

type GachaItemMaster struct {
	ID        int64 `json:"id" db:"id"`
	GachaID   int64 `json:"gachaId" db:"gacha_id"`
	ItemType  int   `json:"itemType" db:"item_type"`
	ItemID    int64 `json:"itemId" db:"item_id"`
	Amount    int   `json:"amount" db:"amount"`
	Weight    int   `json:"weight" db:"weight"`
	CreatedAt int64 `json:"createdAt" db:"created_at"`
}

type ItemMaster struct {
	ID              int64  `json:"id" db:"id"`
	ItemType        int    `json:"itemType" db:"item_type"`
	Name            string `json:"name" db:"name"`
	Description     string `json:"description" db:"description"`
	AmountPerSec    *int   `json:"amountPerSec" db:"amount_per_sec"`
	MaxLevel        *int   `json:"maxLevel" db:"max_level"`
	MaxAmountPerSec *int   `json:"maxAmountPerSec" db:"max_amount_per_sec"`
	BaseExpPerLevel *int   `json:"baseExpPerLevel" db:"base_exp_per_level"`
	GainedExp       *int   `json:"gainedExp" db:"gained_exp"`
	ShorteningMin   *int64 `json:"shorteningMin" db:"shortening_min"`
	// CreatedAt       int64 `json:"createdAt"`
}

type LoginBonusMaster struct {
	ID          int64 `json:"id" db:"id"`
	StartAt     int64 `json:"startAt" db:"start_at"`
	EndAt       int64 `json:"endAt" db:"end_at"`
	ColumnCount int   `json:"columnCount" db:"column_count"`
	Looped      bool  `json:"looped" db:"looped"`
	CreatedAt   int64 `json:"createdAt" db:"created_at"`
}

type LoginBonusRewardMaster struct {
	ID             int64 `json:"id" db:"id"`
	LoginBonusID   int64 `json:"loginBonusId" db:"login_bonus_id"`
	RewardSequence int   `json:"rewardSequence" db:"reward_sequence"`
	ItemType       int   `json:"itemType" db:"item_type"`
	ItemID         int64 `json:"itemId" db:"item_id"`
	Amount         int64 `json:"amount" db:"amount"`
	CreatedAt      int64 `json:"createdAt" db:"created_at"`
}

type PresentAllMaster struct {
	ID                int64  `json:"id" db:"id"`
	RegisteredStartAt int64  `json:"registeredStartAt" db:"registered_start_at"`
	RegisteredEndAt   int64  `json:"registeredEndAt" db:"registered_end_at"`
	ItemType          int    `json:"itemType" db:"item_type"`
	ItemID            int64  `json:"itemId" db:"item_id"`
	Amount            int64  `json:"amount" db:"amount"`
	PresentMessage    string `json:"presentMessage" db:"present_message"`
	CreatedAt         int64  `json:"createdAt" db:"created_at"`
}

type VersionMaster struct {
	ID            int64  `json:"id" db:"id"`
	Status        int    `json:"status" db:"status"`
	MasterVersion string `json:"masterVersion" db:"master_version"`
}
