package messaging

import (
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"veritaserum/src/store"
)

func StartAPIServer(port string, staticFiles fs.FS) {
	r := gin.Default()

	// Serve embedded React build from dist/
	r.GET("/", func(c *gin.Context) {
		f, err := staticFiles.Open("dist/index.html")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		defer f.Close()
		c.DataFromReader(http.StatusOK, -1, "text/html; charset=utf-8", f, nil)
	})
	r.GET("/assets/*filepath", func(c *gin.Context) {
		fp := "dist/assets" + c.Param("filepath")
		f, err := staticFiles.Open(fp)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		defer f.Close()
		ct := "application/octet-stream"
		if strings.HasSuffix(fp, ".js") {
			ct = "application/javascript"
		} else if strings.HasSuffix(fp, ".css") {
			ct = "text/css"
		}
		c.DataFromReader(http.StatusOK, -1, ct, f, nil)
	})

	// ---- Interactions --------------------------------------------------------

	r.GET("/api/interactions", func(c *gin.Context) {
		c.JSON(http.StatusOK, store.GetAllInteractions())
	})

	r.GET("/api/interactions/pending", func(c *gin.Context) {
		c.JSON(http.StatusOK, store.GetPendingInteractions())
	})

	r.POST("/api/interactions/:id/configure", func(c *gin.Context) {
		id := c.Param("id")
		var req struct {
			Name     string                   `json:"name"`
			Response store.InteractionResponse `json:"response"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := store.ConfigureInteraction(id, req.Name, req.Response); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	// ---- Test Cases ----------------------------------------------------------

	r.GET("/api/testcases", func(c *gin.Context) {
		c.JSON(http.StatusOK, store.GetAllTestCases())
	})

	r.POST("/api/testcases", func(c *gin.Context) {
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
			return
		}
		tc := store.CreateTestCase(req.Name, req.Description)
		c.JSON(http.StatusCreated, tc)
	})

	r.PUT("/api/testcases/:id", func(c *gin.Context) {
		id := c.Param("id")
		var req struct {
			Name           string   `json:"name"`
			Description    string   `json:"description"`
			InteractionIDs []string `json:"interactionIds"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := store.UpdateTestCase(id, req.Name, req.Description, req.InteractionIDs); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	r.DELETE("/api/testcases/:id", func(c *gin.Context) {
		if err := store.DeleteTestCase(c.Param("id")); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})

	r.GET("/api/testcases/:id/export", func(c *gin.Context) {
		tc, ok := store.GetTestCase(c.Param("id"))
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		all := store.GetAllInteractions()
		idSet := map[string]bool{}
		for _, id := range tc.InteractionIDs {
			idSet[id] = true
		}
		kept := make([]*store.Interaction, 0)
		for _, i := range all {
			if idSet[i.ID] {
				kept = append(kept, i)
			}
		}
		payload := map[string]interface{}{
			"version":      "2",
			"testCase":     tc.Name,
			"interactions": kept,
		}
		c.Header("Content-Disposition", "attachment; filename=\""+tc.Name+".json\"")
		c.JSON(http.StatusOK, payload)
	})

	// ---- Import --------------------------------------------------------------

	r.POST("/api/import", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var suite struct {
			TestCase     string               `json:"testCase"`
			Interactions []*store.Interaction `json:"interactions"`
		}
		if err := json.Unmarshal(body, &suite); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		tc := store.CreateTestCase(suite.TestCase, "imported")
		ids := make([]string, 0)
		for _, i := range suite.Interactions {
			if i.State == store.StateConfigured {
				existing := store.RegisterInteraction(i.Protocol, i.Key, i.Request)
				if i.Response != nil {
					store.ConfigureInteraction(existing.ID, i.Name, *i.Response)
				}
				ids = append(ids, existing.ID)
			}
		}
		store.UpdateTestCase(tc.ID, tc.Name, tc.Description, ids)
		c.JSON(http.StatusCreated, tc)
	})

	// ---- Schemas -------------------------------------------------------------

	r.GET("/api/schemas", func(c *gin.Context) {
		c.JSON(http.StatusOK, store.GetAllSchemas())
	})

	r.POST("/api/schemas", func(c *gin.Context) {
		var req struct {
			Protocol        string `json:"protocol"`
			TableName       string `json:"tableName"`
			CreateStatement string `json:"createStatement"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.TableName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "protocol and tableName are required"})
			return
		}
		store.UpsertSchema(req.Protocol, req.TableName, req.CreateStatement)
		c.Status(http.StatusNoContent)
	})

	// ---- Persist -------------------------------------------------------------

	r.POST("/api/state/save", func(c *gin.Context) {
		if err := store.SaveState(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		log.Printf("STATE saved to %s", store.StateFileName)
		c.Status(http.StatusNoContent)
	})

	// ---- Health --------------------------------------------------------------

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	log.Printf("API/UI listening on :%s  →  http://localhost:%s/", port, port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("api: %v", err)
	}
}
