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
	geojson "github.com/paulmach/go.geojson"
)

type AreasService struct {
	db *sql.DB
}

func NewAreasService(db *sql.DB) *AreasService {
	return &AreasService{db: db}
}

func (s *AreasService) CreateOrUpdateArea(ctx context.Context, req *models.CreateAreaRequest) (uint64, error) {
	// Start transaction
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		log.Errorf("Error creating transaction: %w", err)
		return 0, err
	}
	defer tx.Rollback()

	// Put the area into areas table
	coords, err := json.MarshalIndent(req.Area.Coordinates, "", "  ")
	if err != nil {
		return 0, err
	}
	tmc, err := time.Parse(time.RFC3339, req.Area.CreatedAt)
	if err != nil {
		return 0, err
	}
	create_ts := tmc.Format(time.DateTime)
	tmu, err := time.Parse(time.RFC3339, req.Area.UpdatedAt)
	if err != nil {
		return 0, err
	}
	update_ts := tmu.Format(time.DateTime)

	rows, err := tx.Query(`SELECT id FROM areas WHERE id = ?`, req.Area.Id)
	if err != nil {
		return 0, err
	}
	areaExists := rows.Next()
	rows.Close()
	log.Infof("Fetch existing area for %d; area exists: %v", req.Area.Id, areaExists)

	var newId int
	if areaExists {
		result, err := tx.Exec(`UPDATE areas
			SET name = ?, description = ?, is_custom = ?, contact_name = ?, type = ?, area_json = ?, created_at = ?, updated_at = ?
			WHERE id = ?`,
			req.Area.Name, req.Area.Description, req.Area.IsCustom, req.Area.ContactName, req.Area.Type, string(coords), create_ts, update_ts, req.Area.Id)
		logResult("updateArea", result, err, true)
		if err != nil {
			return 0, err
		}
		newId = int(req.Area.Id)
		log.Infof("Updated area with ID %d", newId)
	} else {
		result, err := tx.Exec(`INSERT
			INTO areas (name, description, is_custom, contact_name, type, area_json, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			req.Area.Name, req.Area.Description, req.Area.IsCustom, req.Area.ContactName, req.Area.Type, string(coords), create_ts, update_ts)
		logResult("insertArea", result, err, true)
		if err != nil {
			return 0, err
		}
		rows, err := tx.Query("SELECT MAX(id) FROM areas")
		if err != nil {
			return 0, err
		}
		rows.Next()
		if err := rows.Scan(&newId); err != nil {
			return 0, err
		}
		rows.Close()
		log.Infof("Inserted area with id %d", newId)
	}

	// Put emails into emails table
	for _, em := range req.Area.ContractEmails {
		log.Infof("Updating email %v", em)
		rows, err = tx.Query(`SELECT email FROM contact_emails WHERE area_id = ? AND email = ?`, newId, em.Email)
		if err != nil {
			return 0, err
		}
		emailExists := rows.Next()
		rows.Close()
		if emailExists {
			result, err := tx.Exec(`UPDATE contact_emails
				SET consent_report = ? WHERE area_id = ? AND email = ?`, em.ConsentReport, newId, em.Email)
			logResult("updateContactEmails", result, err, false)
			if err != nil {
				return 0, err
			}
			log.Info("Email is updated")
		} else {
			result, err := tx.Exec(`INSERT INTO contact_emails (area_id, email, consent_report)
			  VALUES (?, ?, ?)`, newId, em.Email, em.ConsentReport)
			logResult("insertContactEmails", result, err, true)
			if err != nil {
				return 0, err
			}
			log.Info("Email is inserted")
		}
	}

	// Put a spatial index into areas index table
	result, err := tx.Exec(`DELETE FROM area_index WHERE area_id = ?`, req.Area.Id)
	logResult("deletePreviousAreaIndex", result, err, false)
	if err != nil {
		return 0, err
	}
	areaWKT, err := utils.AreaToWKT(req.Area)
	if err != nil {
		return 0, err
	}
	result, err = tx.Exec("INSERT INTO area_index (area_id, geom) VALUES (?, ST_GeomFromText(?, 4326))", newId, areaWKT)
	logResult("insertAreaIndex", result, err, true)
	if err != nil {
		log.Errorf("%s", areaWKT)
		return 0, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return uint64(newId), nil
}

func (s *AreasService) GetAreas(ctx context.Context, areaIds []uint64, areaType string, viewport *models.ViewPort) ([]*models.Area, error) {
	res := []*models.Area{}

	// Base query
	sqlStr := `SELECT DISTINCT
		a.id, a.name, a.description, a.is_custom, a.contact_name, a.type, a.area_json, a.created_at, a.updated_at
		FROM areas a`

	params := []any{}
	whereConditions := []string{}

	// Add JOIN for viewport filtering if viewport is specified
	if viewport != nil {
		sqlStr += ` JOIN area_index ai ON a.id = ai.area_id`
		whereConditions = append(whereConditions, "ST_Intersects(ST_GeomFromText(?, 4326), ai.geom)")
		params = append(params, utils.ViewPortToWKT(viewport))
	}

	// Add type filter if specified
	if areaType != "" {
		whereConditions = append(whereConditions, "a.type = ?")
		params = append(params, areaType)
	}

	// Add area IDs filter if specified
	if areaIds != nil {
		if len(areaIds) == 0 {
			return res, nil
		}
		qp := make([]string, len(areaIds))
		for i := range areaIds {
			qp[i] = "?"
		}
		ph := strings.Join(qp, ",")
		whereConditions = append(whereConditions, fmt.Sprintf("a.id IN(%s)", ph))
		for _, areaId := range areaIds {
			params = append(params, areaId)
		}
	}

	// Build WHERE clause if we have conditions
	if len(whereConditions) > 0 {
		sqlStr += " WHERE " + strings.Join(whereConditions, " AND ")
	}

	// Add ORDER BY for consistent results
	sqlStr += " ORDER BY a.id"

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
			areaType    string
			areaJson    string
			createdAt   string
			updatedAt   string
		)
		if err := rows.Scan(&id, &name, &description, &isCustom, &contactName, &areaType, &areaJson, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		coords := &geojson.Feature{}
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
			Type:        areaType,
			Coordinates: coords,
			CreatedAt:   cr,
			UpdatedAt:   upd,
		}
		res = append(res, ar)
	}

	return res, nil
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

func (s *AreasService) DeleteArea(ctx context.Context, areaId uint64) error {
	// Start transaction
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		log.Errorf("Error creating transaction for delete area: %w", err)
		return err
	}
	defer tx.Rollback()

	// Check if area exists
	var exists bool
	err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM areas WHERE id = ?)", areaId).Scan(&exists)
	if err != nil {
		log.Errorf("Error checking if area exists: %w", err)
		return err
	}

	if !exists {
		return fmt.Errorf("area with ID %d does not exist", areaId)
	}

	// Delete from areas table - foreign key CASCADE will automatically delete
	// related records from area_index and contact_emails tables
	result, err := tx.Exec("DELETE FROM areas WHERE id = ?", areaId)
	logResult("deleteArea", result, err, true)
	if err != nil {
		return err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		log.Errorf("Error committing delete area transaction: %w", err)
		return err
	}

	log.Infof("Successfully deleted area with ID %d and all related data (via CASCADE)", areaId)
	return nil
}

func logResult(operation string, result sql.Result, err error, isError bool) {
	if err != nil {
		log.Errorf("Error in %s: %v", operation, err)
	} else {
		rowsAffected, _ := result.RowsAffected()
		log.Infof("%s: %d rows affected", operation, rowsAffected)
	}
}
