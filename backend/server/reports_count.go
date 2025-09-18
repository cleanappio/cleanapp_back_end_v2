package server

import (
    "net/http"

    "cleanapp/backend/db"
    "cleanapp/common"

    "github.com/apex/log"
    "github.com/gin-gonic/gin"
)

func GetValidPhysicalReportsCount(c *gin.Context) {
    dbc, err := common.DBConnect()
    if err != nil {
        log.Errorf("Error connecting to DB: %w", err)
        c.Status(http.StatusInternalServerError)
        return
    }
    defer dbc.Close()

    count, err := db.GetValidReportsCount(dbc, "physical")
    if err != nil {
        log.Errorf("Failed to get valid physical reports count: %w", err)
        c.Status(http.StatusInternalServerError)
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "classification": "physical",
        "count":          count,
    })
}

func GetValidDigitalReportsCount(c *gin.Context) {
    dbc, err := common.DBConnect()
    if err != nil {
        log.Errorf("Error connecting to DB: %w", err)
        c.Status(http.StatusInternalServerError)
        return
    }
    defer dbc.Close()

    count, err := db.GetValidReportsCount(dbc, "digital")
    if err != nil {
        log.Errorf("Failed to get valid digital reports count: %w", err)
        c.Status(http.StatusInternalServerError)
        return
    }

    c.JSON(http.StatusOK, gin.H{
        "classification": "digital",
        "count":          count,
    })
}



