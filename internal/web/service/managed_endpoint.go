package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	awg "github.com/mhsanaei/3x-ui/v3/internal/amneziawg"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/mieru"
	"github.com/mhsanaei/3x-ui/v3/internal/naiveproxy"
	webruntime "github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ManagedEndpointSource string

const (
	ManagedEndpointSourceLegacy  ManagedEndpointSource = "legacy-inbound"
	ManagedEndpointSourceManaged ManagedEndpointSource = "managed-endpoint"
)

var ErrManagedIdempotencyConflict = errors.New("managed endpoint idempotency key conflict")

type ManagedTrafficView struct {
	Up   int64 `json:"up" example:"1024"`
	Down int64 `json:"down" example:"2048"`
}

type ManagedHealthView struct {
	Status    model.EndpointStatus `json:"status" example:"active"`
	Message   string               `json:"message"`
	CheckedAt int64                `json:"checkedAt"`
}

type ManagedSecretSummary struct {
	HasSecrets bool     `json:"hasSecrets"`
	Fields     []string `json:"fields"`
}

type ManagedEndpointView struct {
	Id            string                `json:"id" example:"legacy-xray-1"`
	Source        ManagedEndpointSource `json:"source" example:"legacy-inbound"`
	NativeId      int                   `json:"nativeId,omitempty" example:"1"`
	InboundId     *int                  `json:"inboundId,omitempty" example:"1"`
	NodeID        *int                  `json:"nodeId,omitempty" example:"2"`
	RuntimeKind   model.RuntimeKind     `json:"runtimeKind" example:"xray"`
	Protocol      model.ManagedProtocol `json:"protocol" example:"vless"`
	Tag           string                `json:"tag" example:"in-443-tcp"`
	Remark        string                `json:"remark" example:"VLESS-443"`
	Listen        string                `json:"listen"`
	Port          int                   `json:"port" example:"443"`
	Enable        bool                  `json:"enable" example:"true"`
	Status        model.EndpointStatus  `json:"status" example:"active"`
	ClientCount   int                   `json:"clientCount" example:"2"`
	Traffic       ManagedTrafficView    `json:"traffic"`
	Health        ManagedHealthView     `json:"health"`
	SecretSummary ManagedSecretSummary  `json:"secretSummary"`
	NodeName      string                `json:"nodeName,omitempty"`
}

type ManagedEndpointCapability struct {
	RuntimeKind     model.RuntimeKind       `json:"runtimeKind" example:"wireguard"`
	Protocols       []model.ManagedProtocol `json:"protocols"`
	ServerLifecycle bool                    `json:"serverLifecycle"`
	ClientCRUD      bool                    `json:"clientCrud"`
	NativeExport    []string                `json:"nativeExport"`
	Subscription    []string                `json:"subscription"`
	Traffic         bool                    `json:"traffic"`
	Detect          bool                    `json:"detect"`
	FirewallPolicy  bool                    `json:"firewallPolicy"`
}

type ManagedEndpointCapabilities struct {
	RuntimeKinds []ManagedEndpointCapability `json:"runtimeKinds"`
}

type InstallPlanBackendProfile struct {
	Kind               string `json:"kind"`
	ContainerName      string `json:"containerName,omitempty"`
	HostConfigDir      string `json:"hostConfigDir,omitempty"`
	ContainerConfigDir string `json:"containerConfigDir,omitempty"`
}

type InstallPlan struct {
	RuntimeKind         model.RuntimeKind           `json:"runtimeKind"`
	Supported           bool                        `json:"supported"`
	Blocked             bool                        `json:"blocked"`
	RequiresPinnedImage bool                        `json:"requiresPinnedImage"`
	ImageRef            string                      `json:"imageRef,omitempty"`
	Reason              string                      `json:"reason,omitempty"`
	BackendProfiles     []InstallPlanBackendProfile `json:"backendProfiles,omitempty"`
}

type ManagedEndpointService struct{}

type ManagedDriverProvider interface {
	DriverForEndpoint(endpoint model.ManagedEndpoint) (driver.Driver, error)
}

type ManagedEndpointMutationService struct {
	Drivers ManagedDriverProvider
	Secrets ManagedSecretEnvelopeService
}

type RuntimeManagerDriverProvider struct {
	Manager *webruntime.Manager
}

func (p RuntimeManagerDriverProvider) DriverForEndpoint(endpoint model.ManagedEndpoint) (driver.Driver, error) {
	mgr := p.Manager
	if mgr == nil {
		mgr = webruntime.GetManager()
	}
	if mgr == nil {
		return nil, fmt.Errorf("%w: runtime manager unavailable", driver.ErrUnsupportedRuntime)
	}
	if endpoint.NodeID != nil {
		node, err := validateManagedRuntimeNode(*endpoint.NodeID, endpoint.RuntimeKind)
		if err != nil {
			return nil, err
		}
		rt, err := mgr.RemoteFor(node)
		if err != nil {
			return nil, err
		}
		managed, ok := any(rt).(webruntime.ManagedRuntime)
		if !ok {
			return nil, fmt.Errorf("%w: managed remote unavailable", driver.ErrUnsupportedRuntime)
		}
		return managed.Driver(endpoint.RuntimeKind)
	}
	rt, err := mgr.RuntimeFor(nil)
	if err != nil {
		return nil, err
	}
	managed, ok := rt.(webruntime.ManagedRuntime)
	if !ok {
		return nil, fmt.Errorf("%w: managed runtime unavailable", driver.ErrUnsupportedRuntime)
	}
	return managed.Driver(endpoint.RuntimeKind)
}

type ManagedEndpointCreateRequest struct {
	RuntimeKind    model.RuntimeKind        `json:"runtimeKind"`
	Protocol       string                   `json:"protocol"`
	Tag            string                   `json:"tag"`
	Remark         string                   `json:"remark,omitempty"`
	Listen         string                   `json:"listen,omitempty"`
	Port           int                      `json:"port"`
	Enable         *bool                    `json:"enable,omitempty"`
	NodeID         *int                     `json:"nodeId,omitempty"`
	IdempotencyKey string                   `json:"idempotencyKey,omitempty"`
	Config         json.RawMessage          `json:"config,omitempty"`
	AWG            *ManagedAWGConfig        `json:"amneziawg,omitempty"`
	Mieru          *ManagedMieruConfig      `json:"mieru,omitempty"`
	NaiveProxy     *ManagedNaiveProxyConfig `json:"naiveproxy,omitempty"`
}

type ManagedEndpointUpdateRequest struct {
	RuntimeKind    model.RuntimeKind        `json:"runtimeKind,omitempty"`
	Protocol       string                   `json:"protocol,omitempty"`
	Remark         *string                  `json:"remark,omitempty"`
	Listen         *string                  `json:"listen,omitempty"`
	Port           *int                     `json:"port,omitempty"`
	Enable         *bool                    `json:"enable,omitempty"`
	IdempotencyKey string                   `json:"idempotencyKey,omitempty"`
	Config         json.RawMessage          `json:"config,omitempty"`
	AWG            *ManagedAWGConfig        `json:"amneziawg,omitempty"`
	Mieru          *ManagedMieruConfig      `json:"mieru,omitempty"`
	NaiveProxy     *ManagedNaiveProxyConfig `json:"naiveproxy,omitempty"`
}

type ManagedAWGConfig struct {
	Endpoint         string `json:"endpoint,omitempty"`
	IPv4Address      string `json:"ipv4Address,omitempty"`
	IPv4Pool         string `json:"ipv4Pool,omitempty"`
	DNS              string `json:"dns,omitempty"`
	MTU              int    `json:"mtu,omitempty"`
	ServerPrivateKey string `json:"serverPrivateKey,omitempty"`
	ServerPublicKey  string `json:"serverPublicKey,omitempty"`
}

type ManagedMieruConfig struct {
	MTU       int    `json:"mtu,omitempty"`
	Transport string `json:"transport,omitempty"`
}

type ManagedNaiveProxyConfig struct {
	Domain    string `json:"domain,omitempty"`
	ListenIP  string `json:"listenIp,omitempty"`
	ACMEEmail string `json:"acmeEmail,omitempty"`
}

type ManagedEndpointClientCreateRequest struct {
	ClientID       string `json:"clientId,omitempty"`
	Email          string `json:"email"`
	Enable         *bool  `json:"enable,omitempty"`
	Address        string `json:"address,omitempty"`
	Username       string `json:"username,omitempty"`
	Password       string `json:"password,omitempty"`
	PrivateKey     string `json:"privateKey,omitempty"`
	PublicKey      string `json:"publicKey,omitempty"`
	PreSharedKey   string `json:"preSharedKey,omitempty"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
}

type ManagedEndpointClientUpdateRequest struct {
	Email          *string `json:"email,omitempty"`
	Enable         *bool   `json:"enable,omitempty"`
	Address        *string `json:"address,omitempty"`
	Username       *string `json:"username,omitempty"`
	Password       *string `json:"password,omitempty"`
	PrivateKey     *string `json:"privateKey,omitempty"`
	PublicKey      *string `json:"publicKey,omitempty"`
	PreSharedKey   *string `json:"preSharedKey,omitempty"`
	IdempotencyKey string  `json:"idempotencyKey,omitempty"`
}

func (s ManagedEndpointService) List(userId int) ([]ManagedEndpointView, error) {
	db := database.GetDB()
	var native []model.ManagedEndpoint
	if err := db.Where("user_id = ?", userId).Order("id ASC").Find(&native).Error; err != nil {
		return nil, err
	}
	managedInboundIDs := make(map[int]bool)
	for _, row := range native {
		if row.InboundId != nil {
			managedInboundIDs[*row.InboundId] = true
		}
	}

	var inbounds []model.Inbound
	if err := db.Where("user_id = ?", userId).Order("id ASC").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	legacyInboundIDs := make([]int, 0, len(inbounds))
	for _, inbound := range inbounds {
		if !managedInboundIDs[inbound.Id] {
			legacyInboundIDs = append(legacyInboundIDs, inbound.Id)
		}
	}
	legacyCounts, err := batchLegacyClientCounts(db, userId, legacyInboundIDs)
	if err != nil {
		return nil, err
	}
	nativeIDs := make([]int, 0, len(native))
	for _, endpoint := range native {
		nativeIDs = append(nativeIDs, endpoint.Id)
	}
	nativeCounts, nativeTraffic, nativeSecretKinds, err := batchNativeEndpointProjectionData(db, nativeIDs)
	if err != nil {
		return nil, err
	}
	views := make([]ManagedEndpointView, 0, len(inbounds)+len(native))
	for _, inbound := range inbounds {
		if managedInboundIDs[inbound.Id] {
			continue
		}
		views = append(views, legacyInboundView(inbound, legacyCounts[inbound.Id]))
	}
	for _, endpoint := range native {
		views = append(views, nativeEndpointView(endpoint, nativeCounts[endpoint.Id], nativeTraffic[endpoint.Id], nativeSecretKinds[endpoint.Id]))
	}
	return views, nil
}

func (s ManagedEndpointMutationService) Create(ctx context.Context, userId int, req ManagedEndpointCreateRequest) (*ManagedEndpointView, error) {
	if err := req.normalizeConfig(); err != nil {
		return nil, err
	}
	if err := validateManagedEndpointCreate(req); err != nil {
		return nil, err
	}
	reqHash := managedRequestHash("create", req)
	enable := true
	if req.Enable != nil {
		enable = *req.Enable
	}
	var endpoint model.ManagedEndpoint
	replayed := false
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		if req.IdempotencyKey != "" {
			if existing, ok, err := findApplyLog(tx, req.IdempotencyKey, reqHash); err != nil {
				return err
			} else if ok {
				replayed = true
				return tx.First(&endpoint, "id = ? AND user_id = ?", existing.EndpointId, userId).Error
			}
		}
		endpoint = model.ManagedEndpoint{
			UserId:      userId,
			NodeID:      req.NodeID,
			RuntimeKind: req.RuntimeKind,
			Protocol:    model.ManagedProtocol(req.Protocol),
			Tag:         strings.TrimSpace(req.Tag),
			Remark:      strings.TrimSpace(req.Remark),
			Listen:      strings.TrimSpace(req.Listen),
			Port:        req.Port,
			Enable:      enable,
			Status:      model.EndpointApplying,
		}
		if _, err := s.resolveDriver(endpoint); err != nil {
			return err
		}
		if err := tx.Create(&endpoint).Error; err != nil {
			return err
		}
		desired, secrets, err := s.buildDesiredAndSecrets(endpoint, req.AWG, req.Mieru, req.NaiveProxy)
		if err != nil {
			return err
		}
		endpoint.DesiredConfig = desired
		if err := tx.Save(&endpoint).Error; err != nil {
			return err
		}
		if err := upsertManagedSecrets(tx, secrets); err != nil {
			return err
		}
		return createApplyLog(tx, req.IdempotencyKey, endpoint.Id, "create", reqHash, model.EndpointApplying, "")
	})
	if err != nil {
		return nil, err
	}
	if replayed {
		return ManagedEndpointService{}.Get(userId, fmt.Sprintf("managed-%d", endpoint.Id))
	}
	if err := s.applyEndpoint(ctx, &endpoint, "create"); err != nil {
		return nil, err
	}
	return ManagedEndpointService{}.Get(userId, fmt.Sprintf("managed-%d", endpoint.Id))
}

func (s ManagedEndpointMutationService) Update(ctx context.Context, userId int, id string, req ManagedEndpointUpdateRequest) (*ManagedEndpointView, error) {
	if err := req.normalizeConfig(); err != nil {
		return nil, err
	}
	endpointID, err := nativeManagedID(id)
	if err != nil {
		return nil, err
	}
	reqHash := managedRequestHash("update:"+id, req)
	db := database.GetDB()
	var endpoint model.ManagedEndpoint
	replayed := false
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&endpoint, "id = ? AND user_id = ? AND status <> ?", endpointID, userId, model.EndpointDeleted).Error; err != nil {
			return err
		}
		if req.RuntimeKind != "" && req.RuntimeKind != endpoint.RuntimeKind {
			return errors.New("runtime kind is immutable")
		}
		if strings.TrimSpace(req.Protocol) != "" && strings.TrimSpace(req.Protocol) != string(endpoint.Protocol) {
			return errors.New("protocol is immutable")
		}
		if req.IdempotencyKey != "" {
			if existing, ok, err := findApplyLog(tx, req.IdempotencyKey, reqHash); err != nil || ok {
				if ok {
					replayed = true
					return tx.First(&endpoint, "id = ? AND user_id = ?", existing.EndpointId, userId).Error
				}
				return err
			}
		}
		if req.Remark != nil {
			endpoint.Remark = strings.TrimSpace(*req.Remark)
		}
		if req.Listen != nil {
			endpoint.Listen = strings.TrimSpace(*req.Listen)
		}
		if req.Port != nil {
			if *req.Port < 1 || *req.Port > 65535 {
				return errors.New("invalid port")
			}
			endpoint.Port = *req.Port
		}
		if req.Enable != nil {
			endpoint.Enable = *req.Enable
		}
		desired := endpoint.DesiredConfig
		var secrets []model.ManagedSecret
		if req.hasConfig() {
			var err error
			desired, secrets, err = s.buildDesiredAndSecrets(endpoint, req.AWG, req.Mieru, req.NaiveProxy)
			if err != nil {
				return err
			}
		}
		if _, err := s.resolveDriver(endpoint); err != nil {
			return err
		}
		endpoint.DesiredConfig = desired
		endpoint.Status = model.EndpointApplying
		if err := tx.Save(&endpoint).Error; err != nil {
			return err
		}
		if err := upsertManagedSecrets(tx, secrets); err != nil {
			return err
		}
		return createApplyLog(tx, req.IdempotencyKey, endpoint.Id, "update", reqHash, model.EndpointApplying, "")
	})
	if err != nil {
		return nil, err
	}
	if replayed {
		return ManagedEndpointService{}.Get(userId, id)
	}
	if err := s.applyEndpoint(ctx, &endpoint, "update"); err != nil {
		return nil, err
	}
	return ManagedEndpointService{}.Get(userId, id)
}

func (s ManagedEndpointMutationService) Delete(ctx context.Context, userId int, id, idempotencyKey string) error {
	endpointID, err := nativeManagedID(id)
	if err != nil {
		return err
	}
	var endpoint model.ManagedEndpoint
	db := database.GetDB()
	reqHash := managedRequestHash("delete:"+id, nil)
	replayed := false
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&endpoint, "id = ? AND user_id = ?", endpointID, userId).Error; err != nil {
			return err
		}
		if _, ok, err := findApplyLog(tx, idempotencyKey, reqHash); err != nil || ok {
			replayed = ok
			return err
		}
		if endpoint.Status == model.EndpointDeleted {
			return nil
		}
		endpoint.Status = model.EndpointDeleting
		endpoint.Enable = false
		if err := tx.Save(&endpoint).Error; err != nil {
			return err
		}
		return createApplyLog(tx, idempotencyKey, endpoint.Id, "delete", reqHash, model.EndpointDeleting, "")
	}); err != nil {
		return err
	}
	if replayed {
		return nil
	}
	runtimeErr := s.callDriver(ctx, endpoint.RuntimeKind, func(d driver.Driver, inbound *model.Inbound) error {
		_, err := d.Delete(ctx, inbound)
		return err
	}, &endpoint)
	status := model.EndpointDeleted
	code := ""
	if runtimeErr != nil {
		status = model.EndpointFailed
		code = safeManagedErrorCode(runtimeErr)
	}
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.ManagedEndpoint{}).Where("id = ?", endpoint.Id).Updates(map[string]any{"status": status, "last_error": code}).Error; err != nil {
			return err
		}
		return tx.Model(&model.ManagedEndpointApplyLog{}).Where("endpoint_id = ? AND action = ?", endpoint.Id, "delete").Order("id desc").Limit(1).Updates(map[string]any{"status": status, "error": code}).Error
	})
}

func (s ManagedEndpointMutationService) EndpointAction(ctx context.Context, userId int, id, action, idempotencyKey string) (*ManagedEndpointView, any, error) {
	endpointID, err := nativeManagedID(id)
	if err != nil {
		return nil, nil, err
	}
	var endpoint model.ManagedEndpoint
	if err := database.GetDB().First(&endpoint, "id = ? AND user_id = ? AND status <> ?", endpointID, userId, model.EndpointDeleted).Error; err != nil {
		return nil, nil, err
	}
	switch action {
	case "enable", "disable":
		enable := action == "enable"
		endpoint.Enable = enable
		if enable {
			endpoint.Status = model.EndpointApplying
		} else {
			endpoint.Status = model.EndpointDisabled
		}
		if err := database.GetDB().Save(&endpoint).Error; err != nil {
			return nil, nil, err
		}
		if err := s.applyEndpoint(ctx, &endpoint, action); err != nil {
			return nil, nil, err
		}
	case "start", "restart":
		if err := s.callDriver(ctx, endpoint.RuntimeKind, func(d driver.Driver, inbound *model.Inbound) error {
			if action == "restart" {
				return d.Restart(ctx)
			}
			_, err := d.Enable(ctx, inbound)
			return err
		}, &endpoint); err != nil {
			return nil, nil, err
		}
	case "stop":
		if err := s.callDriver(ctx, endpoint.RuntimeKind, func(d driver.Driver, inbound *model.Inbound) error {
			_, err := d.Disable(ctx, inbound)
			return err
		}, &endpoint); err != nil {
			return nil, nil, err
		}
	case "detect":
		d, err := s.resolveDriver(endpoint)
		if err != nil {
			return nil, nil, err
		}
		res, err := d.Detect(ctx)
		return nil, res, err
	case "health":
		d, err := s.resolveDriver(endpoint)
		if err != nil {
			return nil, nil, err
		}
		inbound, err := s.inboundFromDurable(endpoint)
		if err != nil {
			return nil, nil, err
		}
		res, err := d.Health(ctx, inbound)
		return nil, res, err
	case "install-plan":
		return nil, ManagedEndpointService{}.InstallPlan(endpoint.RuntimeKind), nil
	case "install", "update", "uninstall":
		return nil, nil, errors.New("runtime_artifact_precondition_blocked")
	default:
		return nil, nil, fmt.Errorf("unsupported action %q", action)
	}
	view, err := ManagedEndpointService{}.Get(userId, id)
	return view, nil, err
}

func (s ManagedEndpointMutationService) CreateClient(ctx context.Context, userId int, endpointRef string, req ManagedEndpointClientCreateRequest) (model.ManagedEndpointClient, error) {
	if strings.TrimSpace(req.Email) == "" {
		return model.ManagedEndpointClient{}, errors.New("email is required")
	}
	endpointID, err := nativeManagedID(endpointRef)
	if err != nil {
		return model.ManagedEndpointClient{}, err
	}
	enable := true
	if req.Enable != nil {
		enable = *req.Enable
	}
	var endpoint model.ManagedEndpoint
	var client model.ManagedEndpointClient
	reqHash := managedRequestHash("client.create:"+endpointRef, req)
	replayed := false
	err = database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&endpoint, "id = ? AND user_id = ? AND status <> ?", endpointID, userId, model.EndpointDeleted).Error; err != nil {
			return err
		}
		if existing, ok, err := findApplyLog(tx, req.IdempotencyKey, reqHash); err != nil || ok {
			if ok {
				replayed = true
				return tx.First(&client, "endpoint_id = ? AND email = ?", existing.EndpointId, strings.TrimSpace(req.Email)).Error
			}
			return err
		}
		client = model.ManagedEndpointClient{EndpointId: endpoint.Id, ClientId: 0, Email: strings.TrimSpace(req.Email), Enable: enable, State: model.EndpointClientPending}
		if req.ClientID != "" {
			client.PublicIdentity = strings.TrimSpace(req.ClientID)
		} else {
			client.PublicIdentity = uuid.NewString()
		}
		if req.Address != "" {
			client.Address = strings.TrimSpace(req.Address)
		} else if endpoint.RuntimeKind == model.RuntimeAmneziaWG {
			addr, err := allocateAWGIPv4(tx, endpoint.Id)
			if err != nil {
				return err
			}
			client.Address = addr
		}
		if endpoint.RuntimeKind != model.RuntimeAmneziaWG && req.Username != "" {
			client.PublicIdentity = strings.TrimSpace(req.Username)
		}
		if err := tx.Create(&client).Error; err != nil {
			return err
		}
		if endpoint.RuntimeKind != model.RuntimeAmneziaWG && req.Username == "" {
			client.PublicIdentity = fmt.Sprintf("user-%d", client.Id)
			if err := tx.Model(&client).Update("public_identity", client.PublicIdentity).Error; err != nil {
				return err
			}
		}
		secrets, err := s.clientSecrets(endpoint, client, req)
		if err != nil {
			return err
		}
		if err := upsertManagedSecrets(tx, secrets); err != nil {
			return err
		}
		desired, err := s.rebuildDesiredFromDB(tx, endpoint)
		if err != nil {
			return err
		}
		endpoint.DesiredConfig = desired
		endpoint.Status = model.EndpointApplying
		if err := tx.Save(&endpoint).Error; err != nil {
			return err
		}
		return createApplyLog(tx, req.IdempotencyKey, endpoint.Id, "client.create", reqHash, model.EndpointApplying, "")
	})
	if err != nil {
		return model.ManagedEndpointClient{}, err
	}
	if replayed {
		return client, nil
	}
	if err := s.applyEndpoint(ctx, &endpoint, "client.create"); err != nil {
		return model.ManagedEndpointClient{}, err
	}
	return client, nil
}

func (s ManagedEndpointMutationService) ListClients(userId, endpointID int) ([]model.ManagedEndpointClient, error) {
	var endpoint model.ManagedEndpoint
	if err := database.GetDB().First(&endpoint, "id = ? AND user_id = ? AND status <> ?", endpointID, userId, model.EndpointDeleted).Error; err != nil {
		return nil, err
	}
	var rows []model.ManagedEndpointClient
	err := database.GetDB().Where("endpoint_id = ? AND state <> ?", endpointID, model.EndpointClientDeleted).Order("id asc").Find(&rows).Error
	return rows, err
}

func (s ManagedEndpointMutationService) UpdateClient(ctx context.Context, userId int, endpointRef string, clientID int, req ManagedEndpointClientUpdateRequest) (model.ManagedEndpointClient, error) {
	endpointID, err := nativeManagedID(endpointRef)
	if err != nil {
		return model.ManagedEndpointClient{}, err
	}
	var endpoint model.ManagedEndpoint
	var client model.ManagedEndpointClient
	reqHash := managedRequestHash(fmt.Sprintf("client.update:%s:%d", endpointRef, clientID), req)
	replayed := false
	err = database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&endpoint, "id = ? AND user_id = ? AND status <> ?", endpointID, userId, model.EndpointDeleted).Error; err != nil {
			return err
		}
		if err := tx.First(&client, "id = ? AND endpoint_id = ? AND state <> ?", clientID, endpoint.Id, model.EndpointClientDeleted).Error; err != nil {
			return err
		}
		if existing, ok, err := findApplyLog(tx, req.IdempotencyKey, reqHash); err != nil || ok {
			if ok {
				replayed = true
				return tx.First(&client, "id = ? AND endpoint_id = ?", clientID, existing.EndpointId).Error
			}
			return err
		}
		if req.Email != nil {
			client.Email = strings.TrimSpace(*req.Email)
		}
		if req.Enable != nil {
			client.Enable = *req.Enable
		}
		if req.Address != nil {
			client.Address = strings.TrimSpace(*req.Address)
		}
		if req.Username != nil {
			client.PublicIdentity = strings.TrimSpace(*req.Username)
		}
		client.State = model.EndpointClientPending
		if err := tx.Save(&client).Error; err != nil {
			return err
		}
		if req.Password != nil || req.PrivateKey != nil || req.PublicKey != nil || req.PreSharedKey != nil {
			createReq := ManagedEndpointClientCreateRequest{Email: client.Email, Address: client.Address, Username: client.PublicIdentity, Password: deref(req.Password), PrivateKey: deref(req.PrivateKey), PublicKey: deref(req.PublicKey), PreSharedKey: deref(req.PreSharedKey)}
			secrets, err := s.clientSecrets(endpoint, client, createReq)
			if err != nil {
				return err
			}
			if err := upsertManagedSecrets(tx, secrets); err != nil {
				return err
			}
		}
		desired, err := s.rebuildDesiredFromDB(tx, endpoint)
		if err != nil {
			return err
		}
		endpoint.DesiredConfig = desired
		endpoint.Status = model.EndpointApplying
		if err := tx.Save(&endpoint).Error; err != nil {
			return err
		}
		return createApplyLog(tx, req.IdempotencyKey, endpoint.Id, "client.update", reqHash, model.EndpointApplying, "")
	})
	if err != nil {
		return model.ManagedEndpointClient{}, err
	}
	if replayed {
		return client, nil
	}
	if err := s.applyEndpoint(ctx, &endpoint, "client.create"); err != nil {
		return model.ManagedEndpointClient{}, err
	}
	return client, nil
}

func (s ManagedEndpointMutationService) DeleteClient(ctx context.Context, userId int, endpointRef string, clientID int, idempotencyKey string) error {
	endpointID, err := nativeManagedID(endpointRef)
	if err != nil {
		return err
	}
	var endpoint model.ManagedEndpoint
	reqHash := managedRequestHash(fmt.Sprintf("client.delete:%s:%d", endpointRef, clientID), nil)
	replayed := false
	err = database.GetDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&endpoint, "id = ? AND user_id = ? AND status <> ?", endpointID, userId, model.EndpointDeleted).Error; err != nil {
			return err
		}
		if _, ok, err := findApplyLog(tx, idempotencyKey, reqHash); err != nil || ok {
			replayed = ok
			return err
		}
		if err := tx.Model(&model.ManagedEndpointClient{}).Where("id = ? AND endpoint_id = ?", clientID, endpoint.Id).Updates(map[string]any{"state": model.EndpointClientDeleted, "enable": false}).Error; err != nil {
			return err
		}
		desired, err := s.rebuildDesiredFromDB(tx, endpoint)
		if err != nil {
			return err
		}
		endpoint.DesiredConfig = desired
		endpoint.Status = model.EndpointApplying
		if err := tx.Save(&endpoint).Error; err != nil {
			return err
		}
		return createApplyLog(tx, idempotencyKey, endpoint.Id, "client.delete", reqHash, model.EndpointApplying, "")
	})
	if err != nil {
		return err
	}
	if replayed {
		return nil
	}
	return s.applyEndpoint(ctx, &endpoint, "client.create")
}

func (s ManagedEndpointMutationService) ClientExport(userId int, endpointRef string, clientID int) (string, error) {
	endpointID, err := nativeManagedID(endpointRef)
	if err != nil {
		return "", err
	}
	var endpoint model.ManagedEndpoint
	if err := database.GetDB().First(&endpoint, "id = ? AND user_id = ?", endpointID, userId).Error; err != nil {
		return "", err
	}
	var client model.ManagedEndpointClient
	if err := database.GetDB().First(&client, "id = ? AND endpoint_id = ?", clientID, endpoint.Id).Error; err != nil {
		return "", err
	}
	switch endpoint.RuntimeKind {
	case model.RuntimeAmneziaWG:
		clients, err := s.awgClientsFromDB(database.GetDB(), endpoint.Id)
		if err != nil {
			return "", err
		}
		for _, c := range clients {
			if c.ID == client.PublicIdentity {
				return fmt.Sprintf("[Interface]\nPrivateKey = %s\nAddress = %s\nDNS = 1.1.1.1\n", c.PrivateKey, c.IPv4Address), nil
			}
		}
	case model.RuntimeMieru, model.RuntimeNaiveProxy:
		var row model.ManagedSecret
		if err := database.GetDB().First(&row, "owner_type = ? AND owner_id = ? AND secret_kind = ?", "managed_endpoint_client", client.Id, "password").Error; err != nil {
			return "", err
		}
		password, err := s.decryptSecret(row)
		if err != nil {
			return "", err
		}
		return string(password), nil
	}
	return "", fmt.Errorf("client export unavailable")
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (s ManagedEndpointService) Get(userId int, id string) (*ManagedEndpointView, error) {
	list, err := s.List(userId)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Id == id {
			return &list[i], nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (ManagedEndpointService) Capabilities() ManagedEndpointCapabilities {
	return ManagedEndpointCapabilities{RuntimeKinds: []ManagedEndpointCapability{
		phase0Capability(model.RuntimeXray, model.ManagedProtocol(model.VLESS), model.ManagedProtocol(model.VMESS), model.ManagedProtocol(model.Trojan), model.ManagedProtocol(model.Shadowsocks), model.ManagedProtocol(model.WireGuard), model.ManagedProtocol(model.Hysteria), model.ManagedProtocol(model.HTTP), model.ManagedProtocol(model.Mixed), model.ManagedProtocol(model.Tunnel)),
		phase0Capability(model.RuntimeMTProto, model.ManagedProtocol(model.MTProto)),
		phase0Capability(model.RuntimeWireGuard, "wireguard"),
		managedCapability(model.RuntimeAmneziaWG, false, true, "amneziawg"),
		managedCapability(model.RuntimeMieru, false, true, "mieru"),
		managedCapability(model.RuntimeNaiveProxy, false, true, "naiveproxy"),
	}}
}

func (ManagedEndpointService) InstallPlan(kind model.RuntimeKind) InstallPlan {
	if kind != model.RuntimeAmneziaWG {
		return InstallPlan{
			RuntimeKind: kind,
			Supported:   false,
			Blocked:     true,
			Reason:      "install planning is currently defined only for amneziawg",
		}
	}
	return InstallPlan{
		RuntimeKind:         model.RuntimeAmneziaWG,
		Supported:           false,
		Blocked:             true,
		RequiresPinnedImage: true,
		Reason:              "real install is blocked until this repo builds and publishes a reproducible GHCR AWG2 runtime image pinned by digest; current fleets use local amnezia-awg2:latest images with inconsistent IDs",
		BackendProfiles: []InstallPlanBackendProfile{
			{Kind: "docker-amnezia-awg2", ContainerName: "amnezia-awg2", HostConfigDir: "/opt/amnezia/state/amnezia-awg2", ContainerConfigDir: "/opt/amnezia/awg"},
			{Kind: "native-awg", HostConfigDir: "/etc/amnezia/amneziawg"},
		},
	}
}

func phase0Capability(kind model.RuntimeKind, protocols ...model.ManagedProtocol) ManagedEndpointCapability {
	return ManagedEndpointCapability{
		RuntimeKind:     kind,
		Protocols:       protocols,
		ServerLifecycle: false,
		ClientCRUD:      false,
		NativeExport:    []string{},
		Subscription:    []string{},
		Traffic:         false,
		Detect:          false,
		FirewallPolicy:  false,
	}
}

func managedCapability(kind model.RuntimeKind, traffic bool, detect bool, protocols ...model.ManagedProtocol) ManagedEndpointCapability {
	return ManagedEndpointCapability{
		RuntimeKind:     kind,
		Protocols:       protocols,
		ServerLifecycle: true,
		ClientCRUD:      true,
		NativeExport:    []string{string(kind)},
		Subscription:    []string{},
		Traffic:         traffic,
		Detect:          detect,
		FirewallPolicy:  false,
	}
}

func legacyInboundView(inbound model.Inbound, clientCount int) ManagedEndpointView {
	runtimeKind := model.RuntimeXray
	if inbound.Protocol == model.MTProto {
		runtimeKind = model.RuntimeMTProto
	}
	status := model.EndpointActive
	if !inbound.Enable {
		status = model.EndpointDisabled
	}
	return ManagedEndpointView{
		Id:          legacyManagedEndpointID(runtimeKind, inbound.Id),
		Source:      ManagedEndpointSourceLegacy,
		InboundId:   &inbound.Id,
		NodeID:      inbound.NodeID,
		RuntimeKind: runtimeKind,
		Protocol:    model.ManagedProtocol(inbound.Protocol),
		Tag:         inbound.Tag,
		Remark:      inbound.Remark,
		Listen:      inbound.Listen,
		Port:        inbound.Port,
		Enable:      inbound.Enable,
		Status:      status,
		ClientCount: clientCount,
		Traffic:     ManagedTrafficView{Up: inbound.Up, Down: inbound.Down},
		Health:      ManagedHealthView{Status: status},
		SecretSummary: ManagedSecretSummary{
			HasSecrets: inbound.Protocol == model.MTProto,
			Fields:     legacySecretFields(inbound.Protocol),
		},
	}
}

func legacyManagedEndpointID(kind model.RuntimeKind, inboundID int) string {
	return "legacy-" + string(kind) + "-" + strconv.Itoa(inboundID)
}

func legacySecretFields(protocol model.Protocol) []string {
	if protocol == model.MTProto {
		return []string{"clients.secret"}
	}
	return []string{}
}

func batchLegacyClientCounts(db *gorm.DB, userId int, inboundIDs []int) (map[int]int, error) {
	counts := make(map[int]int, len(inboundIDs))
	if len(inboundIDs) == 0 {
		return counts, nil
	}
	var rows []struct {
		InboundId int
		Count     int
	}
	err := db.Table("client_inbounds").
		Select("client_inbounds.inbound_id AS inbound_id, COUNT(*) AS count").
		Joins("JOIN inbounds ON inbounds.id = client_inbounds.inbound_id").
		Where("inbounds.user_id = ? AND client_inbounds.inbound_id IN ?", userId, inboundIDs).
		Group("client_inbounds.inbound_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		counts[row.InboundId] = row.Count
	}
	return counts, nil
}

func batchNativeEndpointProjectionData(db *gorm.DB, endpointIDs []int) (map[int]int, map[int]ManagedTrafficView, map[int][]string, error) {
	counts := make(map[int]int, len(endpointIDs))
	traffic := make(map[int]ManagedTrafficView, len(endpointIDs))
	secretKinds := make(map[int][]string, len(endpointIDs))
	if len(endpointIDs) == 0 {
		return counts, traffic, secretKinds, nil
	}

	var countRows []struct {
		EndpointId int
		Count      int
	}
	if err := db.Model(&model.ManagedEndpointClient{}).
		Select("endpoint_id, COUNT(*) AS count").
		Where("endpoint_id IN ?", endpointIDs).
		Group("endpoint_id").
		Scan(&countRows).Error; err != nil {
		return nil, nil, nil, err
	}
	for _, row := range countRows {
		counts[row.EndpointId] = row.Count
	}

	var trafficRows []struct {
		EndpointId int
		Up         int64
		Down       int64
	}
	if err := db.Model(&model.ManagedEndpointClientTraffic{}).
		Select("endpoint_id, COALESCE(SUM(up), 0) AS up, COALESCE(SUM(down), 0) AS down").
		Where("endpoint_id IN ?", endpointIDs).
		Group("endpoint_id").
		Scan(&trafficRows).Error; err != nil {
		return nil, nil, nil, err
	}
	for _, row := range trafficRows {
		traffic[row.EndpointId] = ManagedTrafficView{Up: row.Up, Down: row.Down}
	}

	var secretRows []struct {
		OwnerId    int
		SecretKind string
	}
	if err := db.Model(&model.ManagedSecret{}).
		Select("owner_id, secret_kind").
		Where("owner_type = ? AND owner_id IN ?", "managed_endpoint", endpointIDs).
		Order("secret_kind ASC").
		Scan(&secretRows).Error; err != nil {
		return nil, nil, nil, err
	}
	for _, row := range secretRows {
		secretKinds[row.OwnerId] = append(secretKinds[row.OwnerId], row.SecretKind)
	}
	return counts, traffic, secretKinds, nil
}

func nativeEndpointView(endpoint model.ManagedEndpoint, clientCount int, traffic ManagedTrafficView, secretKinds []string) ManagedEndpointView {
	sort.Strings(secretKinds)
	status := endpoint.Status
	if status == "" {
		if endpoint.Enable {
			status = model.EndpointActive
		} else {
			status = model.EndpointDisabled
		}
	}
	return ManagedEndpointView{
		Id:          fmt.Sprintf("managed-%d", endpoint.Id),
		Source:      ManagedEndpointSourceManaged,
		NativeId:    endpoint.Id,
		InboundId:   endpoint.InboundId,
		NodeID:      endpoint.NodeID,
		RuntimeKind: endpoint.RuntimeKind,
		Protocol:    endpoint.Protocol,
		Tag:         endpoint.Tag,
		Remark:      endpoint.Remark,
		Listen:      endpoint.Listen,
		Port:        endpoint.Port,
		Enable:      endpoint.Enable,
		Status:      status,
		ClientCount: clientCount,
		Traffic:     traffic,
		Health:      ManagedHealthView{Status: status, CheckedAt: endpoint.LastHealthAt},
		SecretSummary: ManagedSecretSummary{
			HasSecrets: len(secretKinds) > 0,
			Fields:     secretKinds,
		},
	}
}

func ParseManagedEndpointID(id string) (string, error) {
	if strings.HasPrefix(id, "managed-") {
		if _, err := strconv.Atoi(strings.TrimPrefix(id, "managed-")); err != nil {
			return "", err
		}
		return id, nil
	}
	parts := strings.Split(id, "-")
	if len(parts) != 3 || parts[0] != "legacy" {
		return "", errors.New("invalid managed endpoint id")
	}
	if _, err := strconv.Atoi(parts[2]); err != nil {
		return "", err
	}
	return id, nil
}

func validateManagedEndpointCreate(req ManagedEndpointCreateRequest) error {
	if strings.TrimSpace(req.Tag) == "" {
		return errors.New("tag is required")
	}
	if req.Port < 1 || req.Port > 65535 {
		return errors.New("invalid port")
	}
	protocol := model.RuntimeKind(strings.TrimSpace(req.Protocol))
	if req.RuntimeKind != protocol {
		return errors.New("runtime kind and protocol must match")
	}
	switch req.RuntimeKind {
	case model.RuntimeAmneziaWG:
		if req.AWG == nil || req.Mieru != nil || req.NaiveProxy != nil {
			return errors.New("amneziawg config is required")
		}
	case model.RuntimeMieru:
		if req.Mieru == nil || req.AWG != nil || req.NaiveProxy != nil {
			return errors.New("mieru config is required")
		}
	case model.RuntimeNaiveProxy:
		if req.NaiveProxy == nil || req.AWG != nil || req.Mieru != nil {
			return errors.New("naiveproxy config is required")
		}
	default:
		return fmt.Errorf("unsupported runtime kind %q", req.RuntimeKind)
	}
	return nil
}

func nativeManagedID(id string) (int, error) {
	if !strings.HasPrefix(id, "managed-") {
		return 0, errors.New("managed endpoint mutations require native managed id")
	}
	n, err := strconv.Atoi(strings.TrimPrefix(id, "managed-"))
	if err != nil || n <= 0 {
		return 0, errors.New("invalid managed endpoint id")
	}
	return n, nil
}

func (req *ManagedEndpointCreateRequest) normalizeConfig() error {
	if len(req.Config) == 0 {
		return nil
	}
	if req.AWG != nil || req.Mieru != nil || req.NaiveProxy != nil {
		return errors.New("ambiguous managed endpoint config")
	}
	switch req.RuntimeKind {
	case model.RuntimeAmneziaWG:
		var cfg ManagedAWGConfig
		if err := decodePublicManagedConfig(req.Config, &cfg); err != nil {
			return err
		}
		req.AWG = &cfg
	case model.RuntimeMieru:
		var cfg ManagedMieruConfig
		if err := decodePublicManagedConfig(req.Config, &cfg); err != nil {
			return err
		}
		req.Mieru = &cfg
	case model.RuntimeNaiveProxy:
		var cfg ManagedNaiveProxyConfig
		if err := decodePublicManagedConfig(req.Config, &cfg); err != nil {
			return err
		}
		req.NaiveProxy = &cfg
	default:
		return fmt.Errorf("unsupported runtime kind %q", req.RuntimeKind)
	}
	return nil
}

func (req *ManagedEndpointUpdateRequest) normalizeConfig() error {
	if len(req.Config) == 0 {
		return nil
	}
	if req.AWG != nil || req.Mieru != nil || req.NaiveProxy != nil {
		return errors.New("ambiguous managed endpoint config")
	}
	kind := req.RuntimeKind
	if kind == "" {
		if req.Protocol != "" {
			kind = model.RuntimeKind(req.Protocol)
		} else {
			return errors.New("runtime kind is required with config")
		}
	}
	switch kind {
	case model.RuntimeAmneziaWG:
		var cfg ManagedAWGConfig
		if err := decodePublicManagedConfig(req.Config, &cfg); err != nil {
			return err
		}
		req.AWG = &cfg
	case model.RuntimeMieru:
		var cfg ManagedMieruConfig
		if err := decodePublicManagedConfig(req.Config, &cfg); err != nil {
			return err
		}
		req.Mieru = &cfg
	case model.RuntimeNaiveProxy:
		var cfg ManagedNaiveProxyConfig
		if err := decodePublicManagedConfig(req.Config, &cfg); err != nil {
			return err
		}
		req.NaiveProxy = &cfg
	default:
		return fmt.Errorf("unsupported runtime kind %q", kind)
	}
	return nil
}

func (req ManagedEndpointUpdateRequest) hasConfig() bool {
	return len(req.Config) > 0 || req.AWG != nil || req.Mieru != nil || req.NaiveProxy != nil
}

func decodePublicManagedConfig(raw json.RawMessage, dst any) error {
	if hasForbiddenManagedConfigField(raw) {
		return errors.New("managed endpoint config contains forbidden private or runtime field")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := ensureDecoderEOF(dec); err != nil {
		return err
	}
	return nil
}

func ensureDecoderEOF(dec *json.Decoder) error {
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("trailing json")
		}
		return err
	}
	return nil
}

func hasForbiddenManagedConfigField(raw []byte) bool {
	var walk func(any) bool
	walk = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				switch strings.ToLower(k) {
				case "path", "image", "command", "args", "env", "privatekey", "serverprivatekey", "password", "secret", "token":
					return true
				}
				if walk(val) {
					return true
				}
			}
		case []any:
			for _, val := range x {
				if walk(val) {
					return true
				}
			}
		}
		return false
	}
	var v any
	return json.Unmarshal(raw, &v) == nil && walk(v)
}

func validateManagedRuntimeNode(nodeID int, kind model.RuntimeKind) (*model.Node, error) {
	if nodeID <= 0 {
		return nil, errors.New("invalid node id")
	}
	var node model.Node
	if err := database.GetDB().First(&node, "id = ?", nodeID).Error; err != nil {
		return nil, err
	}
	if !node.Enable {
		return nil, errors.New("managed runtime node is disabled")
	}
	if strings.TrimSpace(node.Guid) == "" {
		return nil, errors.New("managed runtime node guid is required")
	}
	if !nodeAdvertisesManagedRuntime(node.RuntimeCapabilities, kind) {
		return nil, fmt.Errorf("managed runtime node does not advertise %s", kind)
	}
	return &node, nil
}

func nodeAdvertisesManagedRuntime(raw string, kind model.RuntimeKind) bool {
	var caps []string
	if err := json.Unmarshal([]byte(raw), &caps); err == nil {
		for _, cap := range caps {
			if model.RuntimeKind(strings.TrimSpace(cap)) == kind {
				return true
			}
		}
	}
	for _, cap := range strings.Split(raw, ",") {
		if model.RuntimeKind(strings.TrimSpace(cap)) == kind {
			return true
		}
	}
	return false
}

func (s ManagedEndpointMutationService) resolveDriver(endpoint model.ManagedEndpoint) (driver.Driver, error) {
	if s.Drivers == nil {
		return nil, fmt.Errorf("%w: managed runtime provider unavailable", driver.ErrUnsupportedRuntime)
	}
	return s.Drivers.DriverForEndpoint(endpoint)
}

func (s ManagedEndpointMutationService) buildDesiredAndSecrets(endpoint model.ManagedEndpoint, awgReq *ManagedAWGConfig, mieruReq *ManagedMieruConfig, naiveReq *ManagedNaiveProxyConfig) (string, []model.ManagedSecret, error) {
	switch endpoint.RuntimeKind {
	case model.RuntimeAmneziaWG:
		return s.buildAWGDesired(endpoint, awgReq)
	case model.RuntimeMieru:
		return s.buildMieruDesired(endpoint, mieruReq)
	case model.RuntimeNaiveProxy:
		return s.buildNaiveDesired(endpoint, naiveReq)
	default:
		return "", nil, fmt.Errorf("unsupported runtime kind %q", endpoint.RuntimeKind)
	}
}

func (s ManagedEndpointMutationService) buildAWGDesired(endpoint model.ManagedEndpoint, req *ManagedAWGConfig) (string, []model.ManagedSecret, error) {
	if req == nil {
		req = &ManagedAWGConfig{}
	}
	server := awg.DefaultServer("awg0", endpoint.Port)
	server.Enable = endpoint.Enable
	server.Endpoint = strings.TrimSpace(req.Endpoint)
	if req.IPv4Address != "" {
		server.IPv4Address = strings.TrimSpace(req.IPv4Address)
	}
	if req.IPv4Pool != "" {
		server.IPv4Pool = strings.TrimSpace(req.IPv4Pool)
	}
	if req.DNS != "" {
		server.DNS = strings.TrimSpace(req.DNS)
	}
	if req.MTU != 0 {
		server.MTU = req.MTU
	}
	privateKey := strings.TrimSpace(req.ServerPrivateKey)
	publicKey := strings.TrimSpace(req.ServerPublicKey)
	if privateKey == "" {
		var err error
		privateKey, err = awg.GenerateKey()
		if err != nil {
			return "", nil, err
		}
	}
	if publicKey == "" {
		publicKey = "managed-public-" + shortHash(privateKey)
	}
	redacted := server
	redacted.PrivateKey = secretRef("managed_endpoint", endpoint.Id, "server.privateKey")
	redacted.PublicKey = publicKey
	raw, err := json.Marshal(awg.DesiredConfig{Server: redacted})
	if err != nil {
		return "", nil, err
	}
	secret, err := s.encryptSecret("managed_endpoint", endpoint.Id, "server.privateKey", []byte(privateKey))
	if err != nil {
		return "", nil, err
	}
	return string(raw), []model.ManagedSecret{secret}, nil
}

func (s ManagedEndpointMutationService) buildMieruDesired(endpoint model.ManagedEndpoint, req *ManagedMieruConfig) (string, []model.ManagedSecret, error) {
	if req == nil {
		req = &ManagedMieruConfig{}
	}
	transport := mieru.TransportTCP
	if strings.EqualFold(req.Transport, string(mieru.TransportUDP)) {
		transport = mieru.TransportUDP
	}
	cfg := mieru.ServerConfig{PortBindings: []mieru.PortBinding{{Port: endpoint.Port, Protocol: transport}}, MTU: req.MTU}
	raw, err := json.Marshal(mieru.RedactConfig(cfg))
	return string(raw), nil, err
}

func (s ManagedEndpointMutationService) buildNaiveDesired(endpoint model.ManagedEndpoint, req *ManagedNaiveProxyConfig) (string, []model.ManagedSecret, error) {
	if req == nil {
		return "", nil, errors.New("naiveproxy config is required")
	}
	listenIP := strings.TrimSpace(req.ListenIP)
	if listenIP == "" {
		listenIP = "0.0.0.0"
	}
	if listenIP == "0.0.0.0" {
		// The runtime validates concrete IPv4 addresses. Store public desired
		// state without accepting arbitrary bind paths or command flags.
		listenIP = "127.0.0.1"
	}
	server := naiveproxy.Server{Endpoint: naiveproxy.Endpoint{Domain: strings.TrimSpace(req.Domain), ListenIP: listenIP, Port: endpoint.Port, ACMEEmail: strings.TrimSpace(req.ACMEEmail)}}
	if err := server.Endpoint.Validate(); err != nil {
		return "", nil, err
	}
	raw, err := json.Marshal(map[string]any{"endpoint": server.Endpoint, "users": []any{}})
	return string(raw), nil, err
}

func (s ManagedEndpointMutationService) encryptSecret(ownerType string, ownerID int, kind string, plaintext []byte) (model.ManagedSecret, error) {
	secrets := s.Secrets
	if secrets.Keys == nil {
		secrets = NewManagedSecretEnvelopeService(nil)
	}
	return secrets.Encrypt(ManagedSecretAAD{OwnerType: ownerType, OwnerId: ownerID, SecretKind: kind}, plaintext)
}

func (s ManagedEndpointMutationService) decryptSecret(row model.ManagedSecret) ([]byte, error) {
	secrets := s.Secrets
	if secrets.Keys == nil {
		secrets = NewManagedSecretEnvelopeService(nil)
	}
	return secrets.Decrypt(row, ManagedSecretAAD{OwnerType: row.OwnerType, OwnerId: row.OwnerId, SecretKind: row.SecretKind})
}

func upsertManagedSecrets(tx *gorm.DB, secrets []model.ManagedSecret) error {
	for _, secret := range secrets {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "owner_type"}, {Name: "owner_id"}, {Name: "secret_kind"}},
			DoUpdates: clause.AssignmentColumns([]string{"envelope_version", "key_id", "nonce", "ciphertext", "fingerprint", "updated_at"}),
		}).Create(&secret).Error; err != nil {
			return err
		}
	}
	return nil
}

func findApplyLog(tx *gorm.DB, key, requestHash string) (model.ManagedEndpointApplyLog, bool, error) {
	var row model.ManagedEndpointApplyLog
	if strings.TrimSpace(key) == "" {
		return row, false, nil
	}
	err := tx.First(&row, "idempotency_key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, false, nil
	}
	if err != nil {
		return row, false, err
	}
	if row.RequestHash != requestHash {
		return row, false, ErrManagedIdempotencyConflict
	}
	return row, true, nil
}

func createApplyLog(tx *gorm.DB, key string, endpointID int, action, requestHash string, status model.EndpointStatus, code string) error {
	if strings.TrimSpace(key) == "" {
		key = uuid.NewString()
	}
	return tx.Create(&model.ManagedEndpointApplyLog{IdempotencyKey: key, EndpointId: endpointID, Action: action, Status: string(status), RequestHash: requestHash, Error: code}).Error
}

func managedRequestHash(scope string, req any) string {
	raw, _ := json.Marshal(struct {
		Scope string `json:"scope"`
		Req   any    `json:"req,omitempty"`
	}{Scope: scope, Req: req})
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:])
}

func (s ManagedEndpointMutationService) applyEndpoint(ctx context.Context, endpoint *model.ManagedEndpoint, action string) error {
	beforeHash := endpoint.LastAppliedHash
	beforeObservedHash := endpoint.LastObservedHash
	runtimeErr := s.callDriver(ctx, endpoint.RuntimeKind, func(d driver.Driver, inbound *model.Inbound) error {
		var err error
		switch action {
		case "create", "client.create":
			_, err = d.Create(ctx, inbound)
		case "update":
			_, err = d.Update(ctx, inbound, inbound)
		case "enable":
			_, err = d.Enable(ctx, inbound)
		case "disable":
			_, err = d.Disable(ctx, inbound)
		default:
			_, err = d.Create(ctx, inbound)
		}
		return err
	}, endpoint)
	status := model.EndpointActive
	code := ""
	if !endpoint.Enable {
		status = model.EndpointDisabled
	}
	if runtimeErr != nil {
		status = model.EndpointFailed
		code = safeManagedErrorCode(runtimeErr)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(endpoint.DesiredConfig)))
	err := database.GetDB().Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"status": status, "last_error": code}
		logUpdates := map[string]any{"status": status, "error": code}
		if runtimeErr == nil {
			updates["last_applied_hash"] = hash
			updates["last_observed_hash"] = hash
			updates["observed_config"] = endpoint.DesiredConfig
			logUpdates["after_hash"] = hash
		} else {
			updates["last_applied_hash"] = beforeHash
			updates["last_observed_hash"] = beforeObservedHash
			logUpdates["after_hash"] = beforeHash
		}
		if err := tx.Model(&model.ManagedEndpoint{}).Where("id = ?", endpoint.Id).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ManagedEndpointApplyLog{}).Where("endpoint_id = ?", endpoint.Id).Order("id desc").Limit(1).Updates(logUpdates).Error; err != nil {
			return err
		}
		if runtimeErr == nil {
			return tx.Model(&model.ManagedEndpointClient{}).Where("endpoint_id = ? AND state <> ?", endpoint.Id, model.EndpointClientDeleted).Update("state", model.EndpointClientApplied).Error
		}
		return nil
	})
	if err != nil {
		return err
	}
	if runtimeErr != nil {
		return fmt.Errorf("managed runtime apply failed: %s", code)
	}
	return nil
}

func (s ManagedEndpointMutationService) callDriver(ctx context.Context, _ model.RuntimeKind, fn func(driver.Driver, *model.Inbound) error, endpoint *model.ManagedEndpoint) error {
	d, err := s.resolveDriver(*endpoint)
	if err != nil {
		return err
	}
	inbound, err := s.inboundFromDurable(*endpoint)
	if err != nil {
		return err
	}
	return fn(d, inbound)
}

func (s ManagedEndpointMutationService) inboundFromDurable(endpoint model.ManagedEndpoint) (*model.Inbound, error) {
	switch endpoint.RuntimeKind {
	case model.RuntimeAmneziaWG:
		var cfg awg.DesiredConfig
		if err := json.Unmarshal([]byte(endpoint.DesiredConfig), &cfg); err != nil {
			return nil, err
		}
		var row model.ManagedSecret
		if err := database.GetDB().First(&row, "owner_type = ? AND owner_id = ? AND secret_kind = ?", "managed_endpoint", endpoint.Id, "server.privateKey").Error; err != nil {
			return nil, err
		}
		plaintext, err := s.decryptSecret(row)
		if err != nil {
			return nil, err
		}
		cfg.Server.PrivateKey = string(plaintext)
		clients, err := s.awgClientsFromDB(database.GetDB(), endpoint.Id)
		if err != nil {
			return nil, err
		}
		cfg.Clients = clients
		raw, _ := json.Marshal(cfg)
		return &model.Inbound{Id: endpoint.Id, Tag: endpoint.Tag, Remark: endpoint.Remark, Listen: endpoint.Listen, Port: endpoint.Port, Protocol: model.Protocol("amneziawg"), Enable: endpoint.Enable, Settings: string(raw)}, nil
	case model.RuntimeMieru, model.RuntimeNaiveProxy:
		settings := endpoint.DesiredConfig
		if endpoint.RuntimeKind == model.RuntimeMieru {
			var cfg mieru.ServerConfig
			if err := json.Unmarshal([]byte(endpoint.DesiredConfig), &cfg); err != nil {
				return nil, err
			}
			cfg.Users = nil
			var rows []model.ManagedEndpointClient
			if err := database.GetDB().Where("endpoint_id = ? AND state <> ?", endpoint.Id, model.EndpointClientDeleted).Order("id asc").Find(&rows).Error; err != nil {
				return nil, err
			}
			for _, c := range rows {
				password, err := s.clientPassword(c.Id)
				if err != nil {
					return nil, err
				}
				cfg.Users = append(cfg.Users, mieru.User{Name: c.PublicIdentity, Password: password})
			}
			raw, _ := json.Marshal(cfg)
			settings = string(raw)
		}
		if endpoint.RuntimeKind == model.RuntimeNaiveProxy {
			var payload struct {
				Endpoint naiveproxy.Endpoint `json:"endpoint"`
			}
			if err := json.Unmarshal([]byte(endpoint.DesiredConfig), &payload); err != nil {
				return nil, err
			}
			var rows []model.ManagedEndpointClient
			if err := database.GetDB().Where("endpoint_id = ? AND state <> ? AND enable = ?", endpoint.Id, model.EndpointClientDeleted, true).Order("id asc").Find(&rows).Error; err != nil {
				return nil, err
			}
			users := make([]naiveproxy.User, 0, len(rows))
			for _, c := range rows {
				password, err := s.clientPassword(c.Id)
				if err != nil {
					return nil, err
				}
				users = append(users, naiveproxy.User{ID: c.PublicIdentity, Username: c.PublicIdentity, Password: password, Enabled: c.Enable})
			}
			raw, _ := json.Marshal(map[string]any{"endpoint": payload.Endpoint, "users": users})
			settings = string(raw)
		}
		return &model.Inbound{Id: endpoint.Id, Tag: endpoint.Tag, Remark: endpoint.Remark, Listen: endpoint.Listen, Port: endpoint.Port, Protocol: model.Protocol(endpoint.RuntimeKind), Enable: endpoint.Enable, Settings: settings}, nil
	default:
		return nil, fmt.Errorf("unsupported runtime kind %q", endpoint.RuntimeKind)
	}
}

func (s ManagedEndpointMutationService) clientPassword(clientID int) (string, error) {
	var row model.ManagedSecret
	if err := database.GetDB().First(&row, "owner_type = ? AND owner_id = ? AND secret_kind = ?", "managed_endpoint_client", clientID, "password").Error; err != nil {
		return "", err
	}
	plaintext, err := s.decryptSecret(row)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (s ManagedEndpointMutationService) clientSecrets(endpoint model.ManagedEndpoint, client model.ManagedEndpointClient, req ManagedEndpointClientCreateRequest) ([]model.ManagedSecret, error) {
	var out []model.ManagedSecret
	switch endpoint.RuntimeKind {
	case model.RuntimeAmneziaWG:
		privateKey := strings.TrimSpace(req.PrivateKey)
		publicKey := strings.TrimSpace(req.PublicKey)
		psk := strings.TrimSpace(req.PreSharedKey)
		var err error
		if privateKey == "" {
			privateKey, err = awg.GenerateKey()
			if err != nil {
				return nil, err
			}
		}
		if publicKey == "" {
			publicKey = "managed-public-" + shortHash(privateKey)
		}
		if psk == "" {
			psk, err = awg.GenerateKey()
			if err != nil {
				return nil, err
			}
		}
		for kind, value := range map[string]string{"privateKey": privateKey, "publicKey": publicKey, "presharedKey": psk} {
			secret, err := s.encryptSecret("managed_endpoint_client", client.Id, kind, []byte(value))
			if err != nil {
				return nil, err
			}
			out = append(out, secret)
		}
	case model.RuntimeMieru, model.RuntimeNaiveProxy:
		password := strings.TrimSpace(req.Password)
		if password == "" {
			password = randomPassword()
		}
		secret, err := s.encryptSecret("managed_endpoint_client", client.Id, "password", []byte(password))
		if err != nil {
			return nil, err
		}
		out = append(out, secret)
	}
	return out, nil
}

func (s ManagedEndpointMutationService) rebuildDesiredFromDB(tx *gorm.DB, endpoint model.ManagedEndpoint) (string, error) {
	switch endpoint.RuntimeKind {
	case model.RuntimeAmneziaWG:
		var cfg awg.DesiredConfig
		if err := json.Unmarshal([]byte(endpoint.DesiredConfig), &cfg); err != nil {
			return "", err
		}
		clients, err := s.awgClientsFromDB(tx, endpoint.Id)
		if err != nil {
			return "", err
		}
		for i := range clients {
			clients[i].PrivateKey = secretRef("managed_endpoint_client", 0, "privateKey")
			clients[i].PresharedKey = secretRef("managed_endpoint_client", 0, "presharedKey")
		}
		cfg.Clients = clients
		raw, err := json.Marshal(cfg)
		return string(raw), err
	case model.RuntimeMieru:
		cfg := mieru.ServerConfig{PortBindings: []mieru.PortBinding{{Port: endpoint.Port, Protocol: mieru.TransportTCP}}}
		var clients []model.ManagedEndpointClient
		if err := tx.Where("endpoint_id = ? AND state <> ?", endpoint.Id, model.EndpointClientDeleted).Order("id asc").Find(&clients).Error; err != nil {
			return "", err
		}
		for _, c := range clients {
			cfg.Users = append(cfg.Users, mieru.User{Name: c.PublicIdentity, Password: secretRef("managed_endpoint_client", c.Id, "password")})
		}
		raw, err := json.Marshal(mieru.RedactConfig(cfg))
		return string(raw), err
	case model.RuntimeNaiveProxy:
		var public struct {
			Endpoint naiveproxy.Endpoint `json:"endpoint"`
		}
		if err := json.Unmarshal([]byte(endpoint.DesiredConfig), &public); err != nil {
			return "", err
		}
		var clients []model.ManagedEndpointClient
		if err := tx.Where("endpoint_id = ? AND state <> ?", endpoint.Id, model.EndpointClientDeleted).Order("id asc").Find(&clients).Error; err != nil {
			return "", err
		}
		users := make([]naiveproxy.UserPublic, 0, len(clients))
		for _, c := range clients {
			users = append(users, naiveproxy.UserPublic{ID: c.PublicIdentity, Username: c.PublicIdentity, Enabled: c.Enable})
		}
		raw, err := json.Marshal(map[string]any{"endpoint": public.Endpoint, "users": users})
		return string(raw), err
	default:
		return "", fmt.Errorf("unsupported runtime kind %q", endpoint.RuntimeKind)
	}
}

func (s ManagedEndpointMutationService) awgClientsFromDB(tx *gorm.DB, endpointID int) ([]awg.Client, error) {
	var rows []model.ManagedEndpointClient
	if err := tx.Where("endpoint_id = ? AND state <> ?", endpointID, model.EndpointClientDeleted).Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	clients := make([]awg.Client, 0, len(rows))
	for _, row := range rows {
		secrets := map[string]string{}
		var secretRows []model.ManagedSecret
		if err := tx.Where("owner_type = ? AND owner_id = ?", "managed_endpoint_client", row.Id).Find(&secretRows).Error; err != nil {
			return nil, err
		}
		for _, secretRow := range secretRows {
			plaintext, err := s.decryptSecret(secretRow)
			if err != nil {
				return nil, err
			}
			secrets[secretRow.SecretKind] = string(plaintext)
		}
		clients = append(clients, awg.Client{ID: row.PublicIdentity, Email: row.Email, PrivateKey: secrets["privateKey"], PublicKey: secrets["publicKey"], PresharedKey: secrets["presharedKey"], IPv4Address: row.Address, AllowedIPs: row.Address, ClientAllowedIPs: "0.0.0.0/0", PersistentKeepalive: 25, Enable: row.Enable})
	}
	return clients, nil
}

func allocateAWGIPv4(tx *gorm.DB, endpointID int) (string, error) {
	var used []string
	if err := tx.Model(&model.ManagedEndpointClient{}).Where("endpoint_id = ?", endpointID).Pluck("address", &used).Error; err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for _, addr := range used {
		seen[strings.TrimSpace(addr)] = true
	}
	prefix := netip.MustParsePrefix("10.66.66.0/24")
	for i := 2; i < 255; i++ {
		addr := netip.AddrFrom4([4]byte{10, 66, 66, byte(i)})
		cidr := addr.String() + "/32"
		if prefix.Contains(addr) && !seen[cidr] {
			return cidr, nil
		}
	}
	return "", errors.New("amneziawg IPv4 pool exhausted")
}

func secretRef(ownerType string, ownerID int, kind string) string {
	if ownerID == 0 {
		return "managed-secret://" + ownerType + "/" + kind
	}
	return fmt.Sprintf("managed-secret://%s/%d/%s", ownerType, ownerID, kind)
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:6])
}

func randomPassword() string {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uuid.NewString() + uuid.NewString()
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func safeManagedErrorCode(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(err.Error())
	for _, c := range []string{"unsupported", "unavailable", "validation", "runtime", "corrupt", "precondition"} {
		if strings.Contains(text, c) {
			return "managed_" + c
		}
	}
	return "managed_runtime_failed"
}
