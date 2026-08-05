package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
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
	g.GET("/install-plan", a.installPlans)
	g.GET("/install-plan/:runtimeKind", a.installPlan)
	g.POST("", a.create)
	g.POST("/", a.create)
	g.POST("/create", a.create)
	g.GET("/:id", a.get)
	g.PUT("/:id", a.update)
	g.PATCH("/:id", a.update)
	g.DELETE("/:id", a.delete)
	g.GET("/:id/clients", a.listClients)
	g.POST("/:id/clients", a.createClient)
	g.PUT("/:id/clients/:clientId", a.updateClient)
	g.PATCH("/:id/clients/:clientId", a.updateClient)
	g.DELETE("/:id/clients/:clientId", a.deleteClient)
	g.POST("/:id/clients/:clientId/actions/:action", a.clientAction)
	g.GET("/:id/clients/:clientId/export", a.exportClient)
	g.POST("/:id/actions/:action", a.action)
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

func (a *ManagedEndpointController) installPlan(c *gin.Context) {
	jsonObj(c, a.managedEndpointService.InstallPlan(model.RuntimeKind(c.Param("runtimeKind"))), nil)
}

func (a *ManagedEndpointController) installPlans(c *gin.Context) {
	jsonObj(c, a.managedEndpointService.InstallPlans(), nil)
}

func (a *ManagedEndpointController) create(c *gin.Context) {
	var req service.ManagedEndpointCreateRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	user := session.GetLoginUser(c)
	view, err := managedEndpointMutations().Create(c.Request.Context(), user.Id, req)
	jsonObj(c, view, err)
}

func (a *ManagedEndpointController) update(c *gin.Context) {
	var req service.ManagedEndpointUpdateRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	user := session.GetLoginUser(c)
	view, err := managedEndpointMutations().Update(c.Request.Context(), user.Id, c.Param("id"), req)
	jsonObj(c, view, err)
}

func (a *ManagedEndpointController) delete(c *gin.Context) {
	user := session.GetLoginUser(c)
	err := managedEndpointMutations().Delete(c.Request.Context(), user.Id, c.Param("id"), c.GetHeader("Idempotency-Key"))
	jsonMsg(c, "", err)
}

func (a *ManagedEndpointController) action(c *gin.Context) {
	user := session.GetLoginUser(c)
	view, obj, err := managedEndpointMutations().EndpointAction(c.Request.Context(), user.Id, c.Param("id"), strings.TrimSpace(c.Param("action")), c.GetHeader("Idempotency-Key"))
	if strings.TrimSpace(c.Param("action")) == "install" || strings.TrimSpace(c.Param("action")) == "update" || strings.TrimSpace(c.Param("action")) == "uninstall" {
		if errors.Is(err, service.ErrManagedRuntimeArtifactBlocked) {
			pureJsonMsg(c, http.StatusPreconditionFailed, false, err.Error())
			return
		}
	}
	if obj != nil {
		jsonObj(c, obj, err)
		return
	}
	jsonObj(c, view, err)
}

func (a *ManagedEndpointController) listClients(c *gin.Context) {
	id, err := service.ParseManagedEndpointID(c.Param("id"))
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	nativeID, err := strconvAtoiManaged(id)
	if err != nil {
		jsonMsg(c, I18nWeb(c, "get"), err)
		return
	}
	user := session.GetLoginUser(c)
	rows, err := managedEndpointMutations().ListClients(user.Id, nativeID)
	jsonObj(c, rows, err)
}

func (a *ManagedEndpointController) createClient(c *gin.Context) {
	var req service.ManagedEndpointClientCreateRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	user := session.GetLoginUser(c)
	client, err := managedEndpointMutations().CreateClient(c.Request.Context(), user.Id, c.Param("id"), req)
	jsonObj(c, client, err)
}

func (a *ManagedEndpointController) updateClient(c *gin.Context) {
	var req service.ManagedEndpointClientUpdateRequest
	if !bindStrictJSON(c, &req) {
		return
	}
	user := session.GetLoginUser(c)
	clientID, err := atoiParam(c.Param("clientId"))
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	client, err := managedEndpointMutations().UpdateClient(c.Request.Context(), user.Id, c.Param("id"), clientID, req)
	jsonObj(c, client, err)
}

func (a *ManagedEndpointController) deleteClient(c *gin.Context) {
	user := session.GetLoginUser(c)
	clientID, err := atoiParam(c.Param("clientId"))
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	err = managedEndpointMutations().DeleteClient(c.Request.Context(), user.Id, c.Param("id"), clientID, c.GetHeader("Idempotency-Key"))
	jsonMsg(c, "", err)
}

func (a *ManagedEndpointController) clientAction(c *gin.Context) {
	user := session.GetLoginUser(c)
	clientID, err := atoiParam(c.Param("clientId"))
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	switch c.Param("action") {
	case "enable", "disable":
		enable := c.Param("action") == "enable"
		req := service.ManagedEndpointClientUpdateRequest{Enable: &enable, IdempotencyKey: c.GetHeader("Idempotency-Key")}
		client, err := managedEndpointMutations().UpdateClient(c.Request.Context(), user.Id, c.Param("id"), clientID, req)
		jsonObj(c, client, err)
	case "export":
		out, err := managedEndpointMutations().ClientExport(user.Id, c.Param("id"), clientID)
		jsonObj(c, out, err)
	case "status":
		rows, err := managedEndpointMutations().ListClients(user.Id, mustManagedNative(c.Param("id")))
		if err != nil {
			jsonObj(c, nil, err)
			return
		}
		for _, row := range rows {
			if row.Id == clientID {
				jsonObj(c, row, nil)
				return
			}
		}
		jsonObj(c, nil, gorm.ErrRecordNotFound)
	default:
		jsonObj(c, nil, fmt.Errorf("unsupported client action %q", c.Param("action")))
	}
}

func (a *ManagedEndpointController) exportClient(c *gin.Context) {
	user := session.GetLoginUser(c)
	clientID, err := atoiParam(c.Param("clientId"))
	if err != nil {
		jsonMsg(c, "", err)
		return
	}
	out, err := managedEndpointMutations().ClientExport(user.Id, c.Param("id"), clientID)
	jsonObj(c, out, err)
}

func managedEndpointMutations() service.ManagedEndpointMutationService {
	return service.ManagedEndpointMutationService{Drivers: service.RuntimeManagerDriverProvider{}}
}

func bindStrictJSON(c *gin.Context, dst any) bool {
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		pureJsonMsg(c, http.StatusBadRequest, false, "invalid managed endpoint request")
		c.Abort()
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		pureJsonMsg(c, http.StatusBadRequest, false, "invalid managed endpoint request")
		c.Abort()
		return false
	}
	return true
}

func strconvAtoiManaged(id string) (int, error) {
	if !strings.HasPrefix(id, "managed-") {
		return 0, fmt.Errorf("clients are available only for native managed endpoints")
	}
	n, err := strconv.Atoi(strings.TrimPrefix(id, "managed-"))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid managed endpoint id")
	}
	return n, nil
}

func mustManagedNative(id string) int {
	n, _ := strconvAtoiManaged(id)
	return n
}

func atoiParam(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid id")
	}
	return n, nil
}
