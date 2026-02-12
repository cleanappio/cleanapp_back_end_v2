package server

import (
	"net/http"

	"cleanapp/backend/db"
	"cleanapp/backend/server/api"

	"github.com/apex/log"
	"github.com/gin-gonic/gin"
)

func CreateAction(c *gin.Context) {
	args := &api.ActionModifyArgs{}

	if err := c.BindJSON(args); err != nil {
		log.Errorf("Failed to get the argument in /create_action call: %w", err)
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /create_action, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	args.Record.Id = randRefGen()

	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}

	response, err := db.CreateAction(dbc, args)
	if err != nil {
		log.Errorf("Failed to create action %w", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}

	c.IndentedJSON(http.StatusOK, response) // 200
}

func UpdateAction(c *gin.Context) {
	args := &api.ActionModifyArgs{}

	if err := c.BindJSON(args); err != nil {
		log.Errorf("Failed to get the argument in /update_action call: %w", err)
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /update_action, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}

	response, err := db.UpdateAction(dbc, args)
	if err != nil {
		log.Errorf("Failed to update action %w", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}

	c.IndentedJSON(http.StatusOK, response) // 200
}

func DeleteAction(c *gin.Context) {
	args := &api.ActionModifyArgs{}

	if err := c.BindJSON(args); err != nil {
		log.Errorf("Failed to get the argument in /delete_action call: %w", err)
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /delete_action, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}

	err = db.DeleteAction(dbc, args)
	if err != nil {
		log.Errorf("Failed to delete action %w", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}

	c.Status(http.StatusOK) // 200
}

func GetAction(c *gin.Context) {
	id, exists := c.GetQuery("id")

	if !exists {
		log.Errorf("The param id should not be empty")
		c.String(http.StatusBadRequest, "The param id should not be empty")
		return
	}

	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}

	response, err := db.GetActions(dbc, id)
	if err != nil {
		log.Errorf("Failed to get action %s with %w", id, err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}

	c.IndentedJSON(http.StatusOK, response) // 200
}

func GetActions(c *gin.Context) {
	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}

	response, err := db.GetActions(dbc, "")
	if err != nil {
		log.Errorf("Failed to get actions with %w", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}

	c.IndentedJSON(http.StatusOK, response) // 200
}

func UpdateUserAction(c *gin.Context) {
	args := &api.UserActionArgs{}

	if err := c.BindJSON(args); err != nil {
		log.Errorf("Failed to get the argument in /update_user_action call: %w", err)
		return
	}

	if args.Version != "2.0" {
		log.Errorf("Bad version in /update_user_action, expected: 2.0, got: %v", args.Version)
		c.String(http.StatusNotAcceptable, "Bad API version, expecting 2.0.") // 406
		return
	}

	dbc, err := getServerDB()
	if err != nil {
		log.Errorf("DB connection error: %w", err)
		return
	}

	if err = db.UpdateUserAction(dbc, args); err != nil {
		log.Errorf("Failed to update user actions with %w", err)
		c.Status(http.StatusInternalServerError) // 500
		return
	}
	c.Status(http.StatusOK) // 200
}
