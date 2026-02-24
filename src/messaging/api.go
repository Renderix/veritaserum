package messaging

import (
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"veritaserum/src/store"
)

func StartAPIServer(port string, uiFiles fs.FS) {
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
		store.MocksMu.RLock()
		list := make([]*store.MockDefinition, 0)
		for _, v := range store.Mocks {
			if v.State == store.StatusPending {
				list = append(list, v)
			}
		}
		store.MocksMu.RUnlock()
		c.JSON(http.StatusOK, list)
	})

	// GET /api/mocks — return all configured mocks
	r.GET("/api/mocks", func(c *gin.Context) {
		store.MocksMu.RLock()
		list := make([]*store.MockDefinition, 0)
		for _, v := range store.Mocks {
			if v.State == store.StatusConfigured {
				list = append(list, v)
			}
		}
		store.MocksMu.RUnlock()
		c.JSON(http.StatusOK, list)
	})

	// POST /api/mocks — create or update a mock (marks as configured)
	r.POST("/api/mocks", func(c *gin.Context) {
		var req store.MockDefinition
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var key string
		if req.Protocol == "POSTGRES" {
			key = store.PostgresKey(req.Query)
		} else if req.Protocol == "MYSQL" {
			key = store.MysqlKey(req.Query)
		} else {
			key = store.HttpKey(req.Method, req.URL)
		}

		store.MocksMu.Lock()
		if _, exists := store.Mocks[key]; !exists {
			store.Mocks[key] = &store.MockDefinition{
				Protocol: req.Protocol,
				Method:   req.Method,
				URL:      req.URL,
				Query:    req.Query,
			}
		}
		store.Mocks[key].StatusCode = req.StatusCode
		store.Mocks[key].LatencyMs = req.LatencyMs
		store.Mocks[key].ResponseBody = req.ResponseBody
		store.Mocks[key].State = store.StatusConfigured
		store.MocksMu.Unlock()

		c.Status(http.StatusNoContent)
	})

	// POST /api/export — persist state to disk
	r.POST("/api/export", func(c *gin.Context) {
		store.MocksMu.RLock()
		data, err := json.MarshalIndent(store.Mocks, "", "  ")
		store.MocksMu.RUnlock()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if err := os.WriteFile(store.StateFile, data, 0644); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("STATE exported to %s", store.StateFile)
		c.Status(http.StatusNoContent)
	})

	log.Printf("API/UI listening on :%s  →  http://localhost:%s/", port, port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("api: %v", err)
	}
}
