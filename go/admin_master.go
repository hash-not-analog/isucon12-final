package main

import (
	"bytes"
	"encoding/csv"
	"io"
	"net/http"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"
)

// adminListMaster マスタデータ閲覧
// GET /admin/master
func (h *Handler) adminListMaster(c echo.Context) error {
	masterVersions := make([]*VersionMaster, 0)
	if err := h.DB.Select(&masterVersions, "SELECT * FROM version_masters"); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	items := make([]*ItemMaster, 0)
	if err := h.DB.Select(&items, "SELECT * FROM item_masters"); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	gachas := make([]*GachaMaster, 0)
	if err := h.DB.Select(&gachas, "SELECT * FROM gacha_masters"); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	gachaItems := make([]*GachaItemMaster, 0)
	if err := h.DB.Select(&gachaItems, "SELECT * FROM gacha_item_masters ORDER BY id ASC"); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	presentAlls := make([]*PresentAllMaster, 0)
	if err := h.DB.Select(&presentAlls, "SELECT * FROM present_all_masters"); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)

	}

	loginBonuses := make([]*LoginBonusMaster, 0)
	if err := h.DB.Select(&loginBonuses, "SELECT * FROM login_bonus_masters"); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)

	}

	loginBonusRewards := make([]*LoginBonusRewardMaster, 0)
	if err := h.DB.Select(&loginBonusRewards, "SELECT * FROM login_bonus_reward_masters"); err != nil {
		return errorResponse(c, http.StatusInternalServerError, err)
	}

	return successResponse(c, &AdminListMasterResponse{
		VersionMaster:     masterVersions,
		Items:             items,
		Gachas:            gachas,
		GachaItems:        gachaItems,
		PresentAlls:       presentAlls,
		LoginBonuses:      loginBonuses,
		LoginBonusRewards: loginBonusRewards,
	})
}

type AdminListMasterResponse struct {
	VersionMaster     []*VersionMaster          `json:"versionMaster"`
	Items             []*ItemMaster             `json:"items"`
	Gachas            []*GachaMaster            `json:"gachas"`
	GachaItems        []*GachaItemMaster        `json:"gachaItems"`
	PresentAlls       []*PresentAllMaster       `json:"presentAlls"`
	LoginBonusRewards []*LoginBonusRewardMaster `json:"loginBonusRewards"`
	LoginBonuses      []*LoginBonusMaster       `json:"loginBonuses"`
}

// adminUpdateMaster マスタデータ更新
// PUT /admin/master
func (h *Handler) adminUpdateMaster(c echo.Context) error {
	var activeMaster *VersionMaster
	eg := errgroup.Group{}

	for _, db := range []*sqlx.DB{h.DB, h.DB2, h.DB3, h.DB4} {
		db := db

		eg.Go(func() error {
			tx, err := db.Beginx()
			if err != nil {
				return errorResponse(c, http.StatusInternalServerError, err)
			}
			defer tx.Rollback() //nolint:errcheck

			// version master
			versionMasterRecs, err := readFormFileToCSV(c, "versionMaster")
			if err != nil {
				if err != ErrNoFormFile {
					return errorResponse(c, http.StatusBadRequest, err)
				}
			}
			if versionMasterRecs != nil {
				data := []map[string]interface{}{}
				for i, v := range versionMasterRecs {
					if i == 0 {
						continue
					}
					data = append(data, map[string]interface{}{
						"id":             v[0],
						"status":         v[1],
						"master_version": v[2],
					})
				}

				query := "INSERT INTO version_masters(id, status, master_version) VALUES (:id, :status, :master_version) ON DUPLICATE KEY UPDATE status=VALUES(status), master_version=VALUES(master_version)"
				if _, err = tx.NamedExec(query, data); err != nil {
					return errorResponse(c, http.StatusInternalServerError, err)
				}
			} else {
				// c.Logger().Debug("Skip Update Master: versionMaster")
			}

			// item
			itemMasterRecs, err := readFormFileToCSV(c, "itemMaster")
			if err != nil {
				if err != ErrNoFormFile {
					return errorResponse(c, http.StatusBadRequest, err)
				}
			}
			if itemMasterRecs != nil {
				data := []map[string]interface{}{}
				for i, v := range itemMasterRecs {
					if i == 0 {
						continue
					}
					data = append(data, map[string]interface{}{
						"id":                 v[0],
						"item_type":          v[1],
						"name":               v[2],
						"description":        v[3],
						"amount_per_sec":     v[4],
						"max_level":          v[5],
						"max_amount_per_sec": v[6],
						"base_exp_per_level": v[7],
						"gained_exp":         v[8],
						"shortening_min":     v[9],
					})
				}

				query := strings.Join([]string{
					"INSERT INTO item_masters(id, item_type, name, description, amount_per_sec, max_level, max_amount_per_sec, base_exp_per_level, gained_exp, shortening_min)",
					"VALUES (:id, :item_type, :name, :description, :amount_per_sec, :max_level, :max_amount_per_sec, :base_exp_per_level, :gained_exp, :shortening_min)",
					"ON DUPLICATE KEY UPDATE item_type=VALUES(item_type), name=VALUES(name), description=VALUES(description), amount_per_sec=VALUES(amount_per_sec), max_level=VALUES(max_level), max_amount_per_sec=VALUES(max_amount_per_sec), base_exp_per_level=VALUES(base_exp_per_level), gained_exp=VALUES(gained_exp), shortening_min=VALUES(shortening_min)",
				}, " ")
				if _, err = tx.NamedExec(query, data); err != nil {
					return errorResponse(c, http.StatusInternalServerError, err)
				}
			} else {
				// c.Logger().Debug("Skip Update Master: itemMaster")
			}

			// gacha
			gachaRecs, err := readFormFileToCSV(c, "gachaMaster")
			if err != nil {
				if err != ErrNoFormFile {
					return errorResponse(c, http.StatusBadRequest, err)
				}
			}
			if gachaRecs != nil {
				data := []map[string]interface{}{}
				for i, v := range gachaRecs {
					if i == 0 {
						continue
					}
					data = append(data, map[string]interface{}{
						"id":            v[0],
						"name":          v[1],
						"start_at":      v[2],
						"end_at":        v[3],
						"display_order": v[4],
						"created_at":    v[5],
					})
				}

				query := strings.Join([]string{
					"INSERT INTO gacha_masters(id, name, start_at, end_at, display_order, created_at)",
					"VALUES (:id, :name, :start_at, :end_at, :display_order, :created_at)",
					"ON DUPLICATE KEY UPDATE name=VALUES(name), start_at=VALUES(start_at), end_at=VALUES(end_at), display_order=VALUES(display_order), created_at=VALUES(created_at)",
				}, " ")
				if _, err = tx.NamedExec(query, data); err != nil {
					return errorResponse(c, http.StatusInternalServerError, err)
				}
			} else {
				// c.Logger().Debug("Skip Update Master: gachaMaster")
			}

			// gacha item
			gachaItemRecs, err := readFormFileToCSV(c, "gachaItemMaster")
			if err != nil {
				if err != ErrNoFormFile {
					return errorResponse(c, http.StatusBadRequest, err)
				}
			}
			if gachaItemRecs != nil {
				data := []map[string]interface{}{}
				for i, v := range gachaItemRecs {
					if i == 0 {
						continue
					}
					data = append(data, map[string]interface{}{
						"id":         v[0],
						"gacha_id":   v[1],
						"item_type":  v[2],
						"item_id":    v[3],
						"amount":     v[4],
						"weight":     v[5],
						"created_at": v[6],
					})
				}

				query := strings.Join([]string{
					"INSERT INTO gacha_item_masters(id, gacha_id, item_type, item_id, amount, weight, created_at)",
					"VALUES (:id, :gacha_id, :item_type, :item_id, :amount, :weight, :created_at)",
					"ON DUPLICATE KEY UPDATE gacha_id=VALUES(gacha_id), item_type=VALUES(item_type), item_id=VALUES(item_id), amount=VALUES(amount), weight=VALUES(weight), created_at=VALUES(created_at)",
				}, " ")
				if _, err = tx.NamedExec(query, data); err != nil {
					return errorResponse(c, http.StatusInternalServerError, err)
				}
			} else {
				// c.Logger().Debug("Skip Update Master: gachaItemMaster")
			}

			// present all
			presentAllRecs, err := readFormFileToCSV(c, "presentAllMaster")
			if err != nil {
				if err != ErrNoFormFile {
					return errorResponse(c, http.StatusBadRequest, err)
				}
			}
			if presentAllRecs != nil {
				data := []map[string]interface{}{}
				for i, v := range presentAllRecs {
					if i == 0 {
						continue
					}
					data = append(data, map[string]interface{}{
						"id":                  v[0],
						"registered_start_at": v[1],
						"registered_end_at":   v[2],
						"item_type":           v[3],
						"item_id":             v[4],
						"amount":              v[5],
						"present_message":     v[6],
						"created_at":          v[7],
					})
				}

				query := strings.Join([]string{
					"INSERT INTO present_all_masters(id, registered_start_at, registered_end_at, item_type, item_id, amount, present_message, created_at)",
					"VALUES (:id, :registered_start_at, :registered_end_at, :item_type, :item_id, :amount, :present_message, :created_at)",
					"ON DUPLICATE KEY UPDATE registered_start_at=VALUES(registered_start_at), registered_end_at=VALUES(registered_end_at), item_type=VALUES(item_type), item_id=VALUES(item_id), amount=VALUES(amount), present_message=VALUES(present_message), created_at=VALUES(created_at)",
				}, " ")
				if _, err = tx.NamedExec(query, data); err != nil {
					return errorResponse(c, http.StatusInternalServerError, err)
				}
			} else {
				// c.Logger().Debug("Skip Update Master: presentAllMaster")
			}

			// login bonuses
			loginBonusRecs, err := readFormFileToCSV(c, "loginBonusMaster")
			if err != nil {
				if err != ErrNoFormFile {
					return errorResponse(c, http.StatusBadRequest, err)
				}
			}
			if loginBonusRecs != nil {
				data := []map[string]interface{}{}
				for i, v := range loginBonusRecs {
					if i == 0 {
						continue
					}
					looped := 0
					if v[4] == "TRUE" {
						looped = 1
					}
					data = append(data, map[string]interface{}{
						"id":           v[0],
						"start_at":     v[1],
						"end_at":       v[2],
						"column_count": v[3],
						"looped":       looped,
						"created_at":   v[5],
					})
				}

				query := strings.Join([]string{
					"INSERT INTO login_bonus_masters(id, start_at, end_at, column_count, looped, created_at)",
					"VALUES (:id, :start_at, :end_at, :column_count, :looped, :created_at)",
					"ON DUPLICATE KEY UPDATE start_at=VALUES(start_at), end_at=VALUES(end_at), column_count=VALUES(column_count), looped=VALUES(looped), created_at=VALUES(created_at)",
				}, " ")
				if _, err = tx.NamedExec(query, data); err != nil {
					return errorResponse(c, http.StatusInternalServerError, err)
				}
			} else {
				// c.Logger().Debug("Skip Update Master: loginBonusMaster")
			}

			// login bonus rewards
			loginBonusRewardRecs, err := readFormFileToCSV(c, "loginBonusRewardMaster")
			if err != nil {
				if err != ErrNoFormFile {
					return errorResponse(c, http.StatusBadRequest, err)
				}
			}
			if loginBonusRewardRecs != nil {
				data := []map[string]interface{}{}
				for i, v := range loginBonusRewardRecs {
					if i == 0 {
						continue
					}
					data = append(data, map[string]interface{}{
						"id":              v[0],
						"login_bonus_id":  v[1],
						"reward_sequence": v[2],
						"item_type":       v[3],
						"item_id":         v[4],
						"amount":          v[5],
						"created_at":      v[6],
					})
				}

				query := strings.Join([]string{
					"INSERT INTO login_bonus_reward_masters(id, login_bonus_id, reward_sequence, item_type, item_id, amount, created_at)",
					"VALUES (:id, :login_bonus_id, :reward_sequence, :item_type, :item_id, :amount, :created_at)",
					"ON DUPLICATE KEY UPDATE login_bonus_id=VALUES(login_bonus_id), reward_sequence=VALUES(reward_sequence), item_type=VALUES(item_type), item_id=VALUES(item_id), amount=VALUES(amount), created_at=VALUES(created_at)",
				}, " ")
				if _, err = tx.NamedExec(query, data); err != nil {
					return errorResponse(c, http.StatusInternalServerError, err)
				}
			} else {
				// c.Logger().Debug("Skip Update Master: loginBonusRewardMaster")
			}

			activeMaster = new(VersionMaster)
			if err = tx.Get(activeMaster, "SELECT * FROM version_masters WHERE status=1"); err != nil {
				return errorResponse(c, http.StatusInternalServerError, err)
			}

			err = tx.Commit()
			if err != nil {
				return errorResponse(c, http.StatusInternalServerError, err)
			}

			return nil
		})
	}

	err := eg.Wait()
	if err != nil {
		return nil
	}

	return successResponse(c, &AdminUpdateMasterResponse{
		VersionMaster: activeMaster,
	})
}

type AdminUpdateMasterResponse struct {
	VersionMaster *VersionMaster `json:"versionMaster"`
}

// readFromFileToCSV ファイルからcsvレコードを取得する
func readFormFileToCSV(c echo.Context, name string) ([][]string, error) {
	file, err := c.FormFile(name)
	if err != nil {
		return nil, ErrNoFormFile
	}

	src, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	buf := new(bytes.Buffer)
	if _, err = io.Copy(buf, src); err != nil {
		return nil, err
	}

	csvReader := csv.NewReader(bytes.NewReader(buf.Bytes()))
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, err
	}

	return records, nil
}
