package controller

import (
	"strings"

	"github.com/mhsanaei/3x-ui/v3/internal/util/common"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"

	"github.com/gin-gonic/gin"
)

type GroupController struct {
	clientService  service.ClientService
	inboundService service.InboundService
}

func NewGroupController(g *gin.RouterGroup) *GroupController {
	a := &GroupController{}
	a.initRouter(g)
	return a
}

func (a *GroupController) initRouter(g *gin.RouterGroup) {
	g.GET("/groups", a.list)
	g.GET("/groups/:name/emails", a.emails)
	g.POST("/groups/create", a.create)
	g.POST("/groups/update", a.update)
	g.POST("/groups/rename", a.rename)
	g.POST("/groups/delete", a.delete)
	g.POST("/groups/resetTraffic", a.resetTraffic)
	g.POST("/groups/applyAssignments", a.applyAssignments)
	g.POST("/groups/bulkAdd", a.bulkAdd)
	g.POST("/groups/bulkRemove", a.bulkRemove)
}

func (a *GroupController) list(c *gin.Context) {
	rows, err := a.clientService.ListGroups()
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, rows, nil)
}

func (a *GroupController) emails(c *gin.Context) {
	name := c.Param("name")
	emails, err := a.clientService.EmailsByGroup(name)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, emails, nil)
}

func (a *GroupController) create(c *gin.Context) {
	var body service.GroupUpsertRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if body.Description == "" && len(body.AssignedInboundIds) == 0 && body.Policy.DefaultTotalGB == 0 && body.Policy.DefaultExpiryTime == 0 && !body.Enable {
		body.Enable = true
	}
	result, err := a.clientService.CreateGroupWithConfig(&a.inboundService, body)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
	notifyClientsChanged()
}

func (a *GroupController) update(c *gin.Context) {
	var body service.GroupUpdateRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	result, err := a.clientService.UpdateGroupWithConfig(&a.inboundService, body)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
	notifyClientsChanged()
}

type groupRenameBody struct {
	OldName string `json:"oldName"`
	NewName string `json:"newName"`
}

func (a *GroupController) rename(c *gin.Context) {
	var body groupRenameBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	result, err := a.clientService.RenameGroupAndApply(&a.inboundService, body.OldName, body.NewName)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
	notifyClientsChanged()
}

type groupDeleteBody struct {
	Name string `json:"name"`
}

func (a *GroupController) delete(c *gin.Context) {
	var body groupDeleteBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	result, err := a.clientService.DeleteGroupAndApply(&a.inboundService, body.Name)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
	notifyClientsChanged()
}

type groupResetTrafficBody struct {
	Name string `json:"name"`
}

func (a *GroupController) resetTraffic(c *gin.Context) {
	var body groupResetTrafficBody
	if err := c.ShouldBindJSON(&body); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if err := a.clientService.ResetGroupTraffic(body.Name); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, gin.H{"name": body.Name}, nil)
	notifyClientsChanged()
}

type bulkAddToGroupRequest struct {
	Emails []string `json:"emails"`
	Group  string   `json:"group"`
}

type groupApplyAssignmentsRequest struct {
	Name string `json:"name"`
}

func (a *GroupController) applyAssignments(c *gin.Context) {
	var req groupApplyAssignmentsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	result, err := a.clientService.ApplyGroupAssignments(&a.inboundService, req.Name)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
	notifyClientsChanged()
}

func (a *GroupController) bulkAdd(c *gin.Context) {
	var req bulkAddToGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	if strings.TrimSpace(req.Group) == "" {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), common.NewError("group name is required"))
		return
	}
	result, err := a.clientService.AddToGroupAndApply(&a.inboundService, req.Emails, req.Group)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
	notifyClientsChanged()
}

type bulkRemoveFromGroupRequest struct {
	Emails []string `json:"emails"`
}

func (a *GroupController) bulkRemove(c *gin.Context) {
	var req bulkRemoveFromGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	result, err := a.clientService.RemoveFromGroupAndApply(&a.inboundService, req.Emails)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "somethingWentWrong"), err)
		return
	}
	jsonObj(c, result, nil)
	notifyClientsChanged()
}
