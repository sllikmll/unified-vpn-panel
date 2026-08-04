package controller

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service/protocolconnections"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ProtocolConnectionController struct {
	BaseController
	service *protocolconnections.Service
}

func NewProtocolConnectionController(g *gin.RouterGroup) *ProtocolConnectionController {
	a := &ProtocolConnectionController{service: protocolconnections.NewService(nil)}
	a.initRouter(g)
	return a
}

func (a *ProtocolConnectionController) initRouter(g *gin.RouterGroup) {
	g.GET("/protocols", a.protocols)
	g.GET("", a.list)
	g.GET("/export.yaml", a.exportYAML)
	g.GET("/:id", a.get)
	g.POST("/import", a.importConnection)
	g.PATCH("/:id", a.update)
	g.DELETE("/:id", a.delete)
	g.POST("/preview", a.preview)
	g.GET("/:id/reveal", a.reveal)
}

func (a *ProtocolConnectionController) protocols(c *gin.Context) {
	jsonObj(c, protocolconnections.Protocols, nil)
}

func (a *ProtocolConnectionController) list(c *gin.Context) {
	resp, err := a.service.List(c.Query("protocol"))
	jsonObj(c, resp, err)
}

func (a *ProtocolConnectionController) get(c *gin.Context) {
	item, err := a.service.Get(c.Param("id"), false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "connection not found"})
		return
	}
	jsonObj(c, item, err)
}

func (a *ProtocolConnectionController) reveal(c *gin.Context) {
	item, err := a.service.Get(c.Param("id"), true)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "connection not found"})
		return
	}
	jsonObj(c, item, err)
}

func (a *ProtocolConnectionController) importConnection(c *gin.Context) {
	var req protocolconnections.ImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, "Failed to import protocol connection", err)
		return
	}
	item, replaced, err := a.service.Import(req)
	if err != nil {
		jsonMsg(c, "Failed to import protocol connection", fmt.Errorf("%s", protocolconnections.Redact(err.Error())))
		return
	}
	status := http.StatusCreated
	if replaced {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"success": true, "obj": gin.H{"connection": item, "replaced": replaced}})
}

func (a *ProtocolConnectionController) update(c *gin.Context) {
	var req protocolconnections.UpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, "Failed to update protocol connection", err)
		return
	}
	item, err := a.service.Update(c.Param("id"), req)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "connection not found"})
		return
	}
	jsonObj(c, item, err)
}

func (a *ProtocolConnectionController) delete(c *gin.Context) {
	err := a.service.Delete(c.Param("id"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "msg": "connection not found"})
		return
	}
	jsonMsg(c, "Deleted", err)
}

func (a *ProtocolConnectionController) preview(c *gin.Context) {
	block, err := a.service.ManagedBlock()
	if err != nil {
		jsonMsg(c, "Failed to preview protocol connections", err)
		return
	}
	jsonObj(c, gin.H{"ok": true, "block": block, "configPreview": "proxies:\n" + block}, nil)
}

func (a *ProtocolConnectionController) exportYAML(c *gin.Context) {
	yml, err := a.service.ExportYAML()
	if err != nil {
		jsonMsg(c, "Failed to export protocol connections", err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="protocol-connections.yaml"`)
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", []byte(strings.TrimRight(yml, "\n")+"\n"))
}
