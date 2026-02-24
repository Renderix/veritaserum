package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func StartAPIServer(port string) {
	r := gin.Default()

	// Serve embedded UI
	r.GET("/", func(c *gin.Context) {
		f, err := uiFiles.Open("ui/index.html")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		defer f.Close()
		c.DataFromReader(http.StatusOK, -1, "text/html; charset=utf-8", f, nil)
	})

	// GET /api/pending — return mocks with state=pending
	r.GET("/api/pending", func(c *gin.Context) {
		mocksMu.RLock()
		list := make([]*MockDefinition, 0)
		for _, v := range mocks {
			if v.State == StatusPending {
				list = append(list, v)
			}
		}
		mocksMu.RUnlock()
		c.JSON(http.StatusOK, list)
	})

	// GET /api/mocks — return all configured mocks
	r.GET("/api/mocks", func(c *gin.Context) {
		mocksMu.RLock()
		list := make([]*MockDefinition, 0)
		for _, v := range mocks {
			if v.State == StatusConfigured {
				list = append(list, v)
			}
		}
		mocksMu.RUnlock()
		c.JSON(http.StatusOK, list)
	})

	// POST /api/mocks — create or update a mock (marks as configured)
	r.POST("/api/mocks", func(c *gin.Context) {
		var req MockDefinition
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var key string
		if req.Protocol == "POSTGRES" {
			key = postgresKey(req.Query)
		} else {
			key = httpKey(req.Method, req.URL)
		}

		mocksMu.Lock()
		if _, exists := mocks[key]; !exists {
			mocks[key] = &MockDefinition{
				Protocol: req.Protocol,
				Method:   req.Method,
				URL:      req.URL,
				Query:    req.Query,
			}
		}
		mocks[key].StatusCode = req.StatusCode
		mocks[key].LatencyMs = req.LatencyMs
		mocks[key].ResponseBody = req.ResponseBody
		mocks[key].State = StatusConfigured
		mocksMu.Unlock()

		c.Status(http.StatusNoContent)
	})

	// POST /api/export — persist state to disk
	r.POST("/api/export", func(c *gin.Context) {
		mocksMu.RLock()
		data, err := json.MarshalIndent(mocks, "", "  ")
		mocksMu.RUnlock()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := os.WriteFile(stateFile, data, 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("STATE exported to %s", stateFile)
		c.Status(http.StatusNoContent)
	})

	log.Printf("API/UI listening on :%s  →  http://localhost:%s/", port, port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("api: %v", err)
	}
}
