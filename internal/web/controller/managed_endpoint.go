package controller

import (
	"errors"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
	"github.com/mhsanaei/3x-ui/v3/internal/web/session"
)

type ManagedEndpointController struct {
	managedEndpointService service.ManagedEndpointService
}

func NewManagedEndpointController(g *gin.RouterGroup) *ManagedEndpointController {
	a := &ManagedEndpointController{}
	a.initRouter(g)
	return a
}

func (a *ManagedEndpointController) initRouter(g *gin.RouterGroup) {
	g.GET("/list", a.list)
	g.GET("/capabilities", a.capabilities)
	g.GET("/:id", a.get)
}

func (a *ManagedEndpointController) list(c *gin.Context) {
	user := session.GetLoginUser(c)
	endpoints, err := a.managedEndpointService.List(user.Id)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, endpoints, nil)
}

func (a *ManagedEndpointController) get(c *gin.Context) {
	id, err := service.ParseManagedEndpointID(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	user := session.GetLoginUser(c)
	endpoint, err := a.managedEndpointService.Get(user.Id, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, endpoint, nil)
}

func (a *ManagedEndpointController) capabilities(c *gin.Context) {
	jsonObj(c, a.managedEndpointService.Capabilities(), nil)
}
