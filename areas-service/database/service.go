package database

import (
	"areas-service/models"
	"areas-service/utils"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/apex/log"
)

type AreasService struct {
	db *sql.DB
}

func NewAreasService(db *sql.DB) *AreasService {
	return &AreasService{db: db}
}

func (s *AreasService) CreateOrUpdateArea(ctx context.Context, req *models.CreateAreaRequest) error {
	// Start transaction
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		log.Errorf("Error creating transaction: %w", err)
		return err
	}
	defer tx.Rollback()

	// Put the area into areas table
	coords, err := json.MarshalIndent(req.Area.Coordinates, "", "  ")
	if err != nil {
		return err
	}
	tmc, err := time.Parse(time.RFC3339, req.Area.CreatedAt)
	if err != nil {
		return err
	}
	create_ts := tmc.Format(time.DateTime)
	tmu, err := time.Parse(time.RFC3339, req.Area.UpdatedAt)
	if err != nil {
		return err
	}
	update_ts := tmu.Format(time.DateTime)

	rows, err := tx.Query(`SELECT id FROM areas WHERE id = ?`, req.Area.Id)
	if err != nil {
		return err
	}
	areaExists := rows.Next()
	rows.Close()
	log.Infof("Fetch existing area for %d; area exists: %v", req.Area.Id, areaExists)

	var newId int
	if areaExists {
		result, err := tx.Exec(`UPDATE areas
			SET name = ?, description = ?, is_custom = ?, contact_name = ?, area_json = ?, created_at = ?, updated_at = ?
			WHERE id = ?`,
			req.Area.Name, req.Area.Description, req.Area.IsCustom, req.Area.ContactName, string(coords), create_ts, update_ts, req.Area.Id)
		logResult("updateArea", result, err, true)
		if err != nil {
			return err
		}
		newId = int(req.Area.Id)
		log.Infof("Updated area with ID %d", newId)
	} else {
		result, err := tx.Exec(`INSERT
			INTO areas (name, description, is_custom, contact_name, area_json, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			req.Area.Name, req.Area.Description, req.Area.IsCustom, req.Area.ContactName, string(coords), create_ts, update_ts)
		logResult("insertArea", result, err, true)
		if err != nil {
			return err
		}
		rows, err := tx.Query("SELECT MAX(id) FROM areas")
		if err != nil {
			return err
		}
		rows.Next()
		if err := rows.Scan(&newId); err != nil {
			return err
		}
		rows.Close()
		log.Infof("Inserted area with id %d", newId)
	}

	// Put emails into emails table
	for _, em := range req.Area.ContractEmails {
		log.Infof("Updating email %v", em)
		rows, err = tx.Query(`SELECT email FROM contact_emails WHERE area_id = ? AND email = ?`, newId, em.Email)
		if err != nil {
			return err
		}
		emailExists := rows.Next()
		rows.Close()
		if emailExists {
			result, err := tx.Exec(`UPDATE contact_emails
				SET consent_report = ? WHERE area_id = ? AND email = ?`, em.ConsentReport, newId, em.Email)
			logResult("updateContactEmails", result, err, false)
			if err != nil {
				return err
			}
			log.Info("Email is updated")
		} else {
			result, err := tx.Exec(`INSERT INTO contact_emails (area_id, email, consent_report)
			  VALUES (?, ?, ?)`, newId, em.Email, em.ConsentReport)
			logResult("insertContactEmails", result, err, true)
			if err != nil {
				return err
			}
			log.Info("Email is inserted")
		}
	}

	// Put a spatial index into areas index table
	result, err := tx.Exec(`DELETE FROM area_index WHERE area_id = ?`, req.Area.Id)
	logResult("deletePreviousAreaIndex", result, err, false)
	if err != nil {
		return err
	}
	areaWKT, err := utils.AreaToWKT(req.Area)
	if err != nil {
		return err
	}
	result, err = tx.Exec("INSERT INTO area_index (area_id, geom) VALUES (?, ST_GeomFromText(?, 4326))", newId, areaWKT)
	logResult("insertAreaIndex", result, err, true)
	if err != nil {
		log.Errorf("%s", areaWKT)
		return err
	}

	// Commit transaction
	return tx.Commit()
}

func (s *AreasService) GetAreas(ctx context.Context, areaIds []uint64) ([]*models.Area, error) {
	res := []*models.Area{}

	sqlStr := `SELECT
	 	id, name, description, is_custom, contact_name, area_json, created_at, updated_at
		FROM areas`
	params := []any{}

	if areaIds != nil {
		if len(areaIds) == 0 {
			return res, nil
		}
		qp := make([]string, len(areaIds))
		for i := range areaIds {
			qp[i] = "?"
		}
		ph := strings.Join(qp, ",")
		sqlStr = fmt.Sprintf(`SELECT
			id, name, description, is_custom, contact_name, area_json, created_at, updated_at
			FROM areas
			WHERE id IN(%s)`, ph)
		params = make([]any, len(areaIds))
		for i, areaId := range areaIds {
			params[i] = areaId
		}
	}

	rows, err := s.db.QueryContext(ctx, sqlStr, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id          uint64
			name        string
			description string
			isCustom    bool
			contactName string
			areaJson    string
			createdAt   string
			updatedAt   string
		)
		if err := rows.Scan(&id, &name, &description, &isCustom, &contactName, &areaJson, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		coords := &models.Area{}
		tc, _ := time.Parse(time.DateTime, createdAt)
		cr := tc.Format(time.RFC3339)
		tu, _ := time.Parse(time.DateTime, updatedAt)
		upd := tu.Format(time.RFC3339)

		err = json.Unmarshal([]byte(areaJson), coords)
		if err != nil {
			return nil, err
		}

		ar := &models.Area{
			Id:          id,
			Name:        name,
			Description: description,
			IsCustom:    isCustom,
			ContactName: contactName,
			Coordinates: coords.Coordinates,
			CreatedAt:   cr,
			UpdatedAt:   upd,
		}
		res = append(res, ar)
	}

	return res, nil
}

func (s *AreasService) GetAreaIdsForViewport(ctx context.Context, vp *models.ViewPort) ([]uint64, error) {
	if vp == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT area_id FROM area_index WHERE ST_Intersects(ST_GeomFromText(?, 4326), geom)", utils.ViewPortToWKT(vp))
	if err != nil {
		return nil, err
	}
	ret := []uint64{}
	for rows.Next() {
		var areaId uint64
		if err := rows.Scan(&areaId); err != nil {
			rows.Close()
			return nil, err
		}
		ret = append(ret, areaId)
	}
	rows.Close()

	return ret, nil
}

func (s *AreasService) UpdateConsent(ctx context.Context, req *models.UpdateConsentRequest) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE contact_emails SET consent_report = ? WHERE email = ?",
		req.ContactEmail.ConsentReport, req.ContactEmail.Email,
	)
	logResult("Error updating contact emails", res, err, false)
	return err
}

func (s *AreasService) GetAreasCount(ctx context.Context) (uint64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT COUNT(*) FROM areas")
	if err != nil {
		return 0, err
	}

	var cnt uint64
	for rows.Next() {
		if err := rows.Scan(&cnt); err != nil {
			return 0, err
		}
	}

	return cnt, nil
}

func logResult(operation string, result sql.Result, err error, isError bool) {
	if err != nil {
		log.Errorf("Error in %s: %v", operation, err)
	} else {
		rowsAffected, _ := result.RowsAffected()
		log.Infof("%s: %d rows affected", operation, rowsAffected)
	}
}
