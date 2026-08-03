package controller

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/middleware"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

type ConfigProfileController struct {
	profileService service.ConfigProfileService
}

func NewConfigProfileController(g *gin.RouterGroup) *ConfigProfileController {
	a := &ConfigProfileController{}
	a.initRouter(g)
	return a
}

func (a *ConfigProfileController) initRouter(g *gin.RouterGroup) {
	g.GET("/list", a.list)
	g.GET("/get/:id", a.get)
	g.POST("/add", a.add)
	g.POST("/update/:id", a.update)
	g.POST("/clone/:id", a.clone)
	g.POST("/del/:id", a.del)
	g.POST("/setEnable/:id", a.setEnable)
}

func (a *ConfigProfileController) list(c *gin.Context) {
	profiles, err := a.profileService.GetAll()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.list"), err)
		return
	}
	jsonObj(c, profiles, nil)
}

func (a *ConfigProfileController) get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	profile, err := a.profileService.Get(id)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.obtain"), err)
		return
	}
	jsonObj(c, profile, nil)
}

func (a *ConfigProfileController) add(c *gin.Context) {
	req, ok := middleware.BindJSONAndValidate[model.ConfigProfile](c)
	if !ok {
		return
	}
	created, err := a.profileService.Create(req)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.add"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.profiles.toasts.add"), created, nil)
}

func (a *ConfigProfileController) update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.update"), err)
		return
	}
	req, ok := middleware.BindJSONAndValidate[model.ConfigProfile](c)
	if !ok {
		return
	}
	updated, err := a.profileService.Update(id, req)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.update"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.profiles.toasts.update"), updated, nil)
}

func (a *ConfigProfileController) clone(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.clone"), err)
		return
	}
	var req struct {
		Name string `json:"name" form:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.clone"), err)
		return
	}
	created, err := a.profileService.Clone(id, req.Name)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.clone"), err)
		return
	}
	jsonMsgObj(c, I18nWeb(c, "pages.profiles.toasts.clone"), created, nil)
}

func (a *ConfigProfileController) del(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.delete"), err)
		return
	}
	if err := a.profileService.Delete(id); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.delete"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.delete"), nil)
}

func (a *ConfigProfileController) setEnable(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.update"), err)
		return
	}
	var req struct {
		Enabled bool `json:"enabled" form:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.update"), err)
		return
	}
	if err := a.profileService.SetEnabled(id, req.Enabled); err != nil {
		jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.update"), err)
		return
	}
	jsonMsg(c, I18nWeb(c, "pages.profiles.toasts.update"), nil)
}
