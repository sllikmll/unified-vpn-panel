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
	"net/url"
	"sort"
	"strconv"
	"strings"

	awg "github.com/mhsanaei/3x-ui/v3/internal/amneziawg"
	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/mieru"
	"github.com/mhsanaei/3x-ui/v3/internal/naiveproxy"
	wgutil "github.com/mhsanaei/3x-ui/v3/internal/util/wireguard"
	webruntime "github.com/mhsanaei/3x-ui/v3/internal/web/runtime"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/provisioner"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ManagedEndpointSource string

const (
	ManagedEndpointSourceLegacy  ManagedEndpointSource = "legacy-inbound"
	ManagedEndpointSourceManaged ManagedEndpointSource = "managed-endpoint"
)

var (
	ErrManagedIdempotencyConflict    = errors.New("managed endpoint idempotency key conflict")
	ErrManagedRuntimeArtifactBlocked = provisioner.ErrArtifactBlocked
	ErrManagedSecretMissing          = errors.New("managed secret missing")
)

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

type ManagedClientExportSubscriptions struct {
	Raw   string `json:"raw,omitempty"`
	JSON  string `json:"json,omitempty"`
	Clash string `json:"clash,omitempty"`
}

type ManagedClientExportResponse struct {
	Content       string                           `json:"content,omitempty"`
	Filename      string                           `json:"filename,omitempty"`
	Subscriptions ManagedClientExportSubscriptions `json:"subscriptions,omitempty"`
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
	ArtifactRef         string                      `json:"artifactRef,omitempty"`
	Version             string                      `json:"version,omitempty"`
	Reason              string                      `json:"reason,omitempty"`
	Capabilities        []string                    `json:"capabilities,omitempty"`
	BackendProfiles     []InstallPlanBackendProfile `json:"backendProfiles,omitempty"`
}

type ManagedEndpointService struct{}

type ManagedDriverProvider interface {
	DriverForEndpoint(endpoint model.ManagedEndpoint) (driver.Driver, error)
	ProvisionerForEndpoint(endpoint model.ManagedEndpoint) (provisioner.Provisioner, error)
}

type ManagedEndpointMutationService struct {
	Drivers ManagedDriverProvider
	Secrets ManagedSecretEnvelopeService
}

type RuntimeManagerDriverProvider struct {
	Manager *webruntime.Manager
}

func (p RuntimeManagerDriverProvider) DriverForEndpoint(endpoint model.ManagedEndpoint) (driver.Driver, error) {
	managed, err := p.managedRuntime(endpoint)
	if err != nil {
		return nil, err
	}
	return managed.Driver(endpoint.RuntimeKind)
}

func (p RuntimeManagerDriverProvider) ProvisionerForEndpoint(endpoint model.ManagedEndpoint) (provisioner.Provisioner, error) {
	managed, err := p.managedRuntime(endpoint)
	if err != nil {
		return nil, err
	}
	return managed.Provisioner(), nil
}

func (p RuntimeManagerDriverProvider) managedRuntime(endpoint model.ManagedEndpoint) (webruntime.ManagedRuntime, error) {
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
		return managed, nil
	}
	rt, err := mgr.RuntimeFor(nil)
	if err != nil {
		return nil, err
	}
	managed, ok := rt.(webruntime.ManagedRuntime)
	if !ok {
		return nil, fmt.Errorf("%w: managed runtime unavailable", driver.ErrUnsupportedRuntime)
	}
	return managed, nil
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
	Endpoint            string `json:"endpoint,omitempty"`
	InterfaceName       string `json:"interfaceName,omitempty"`
	ListenPort          int    `json:"listenPort,omitempty"`
	IPv4Address         string `json:"ipv4Address,omitempty"`
	IPv4Pool            string `json:"ipv4Pool,omitempty"`
	DNS                 string `json:"dns,omitempty"`
	MTU                 int    `json:"mtu,omitempty"`
	ClientAllowedIPs    string `json:"clientAllowedIPs,omitempty"`
	PersistentKeepalive int    `json:"persistentKeepalive,omitempty"`
	Jc                  int    `json:"jc,omitempty"`
	Jmin                int    `json:"jmin,omitempty"`
	Jmax                int    `json:"jmax,omitempty"`
	S1                  int    `json:"s1,omitempty"`
	S2                  int    `json:"s2,omitempty"`
	S3                  int    `json:"s3,omitempty"`
	S4                  int    `json:"s4,omitempty"`
	H1                  string `json:"h1,omitempty"`
	H2                  string `json:"h2,omitempty"`
	H3                  string `json:"h3,omitempty"`
	H4                  string `json:"h4,omitempty"`
	ServerPrivateKey    string `json:"serverPrivateKey,omitempty"`
	ServerPublicKey     string `json:"serverPublicKey,omitempty"`
}

type ManagedMieruConfig struct {
	Host         string                    `json:"host,omitempty"`
	MTU          int                       `json:"mtu,omitempty"`
	Transport    string                    `json:"transport,omitempty"`
	PortBindings []ManagedMieruPortBinding `json:"portBindings,omitempty"`
}

type ManagedMieruPortBinding struct {
	Port      int    `json:"port,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	PortRange string `json:"portRange,omitempty"`
}

type ManagedNaiveProxyConfig struct {
	Domain    string `json:"domain,omitempty"`
	SNI       string `json:"sni,omitempty"`
	ListenIP  string `json:"listenIp,omitempty"`
	Port      int    `json:"port,omitempty"`
	TLSMode   string `json:"tlsMode,omitempty"`
	ACMEEmail string `json:"acmeEmail,omitempty"`
}

type ManagedEndpointClientCreateRequest struct {
	ClientID       string `json:"clientId,omitempty"`
	SubID          string `json:"subId,omitempty"`
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
	SubID          *string `json:"subId,omitempty"`
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
	if req.Mieru != nil && strings.TrimSpace(req.Mieru.Host) != "" {
		host, err := normalizeManagedMieruHost(req.Mieru.Host)
		if err != nil {
			return nil, err
		}
		req.Listen = host
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
		tag := strings.TrimSpace(req.Tag)
		var tombstone model.ManagedEndpoint
		revive := false
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND tag = ? AND status = ?", userId, tag, model.EndpointDeleted).Order("id DESC").First(&tombstone).Error; err == nil {
			revive = true
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		endpoint = model.ManagedEndpoint{
			UserId:      userId,
			NodeID:      req.NodeID,
			RuntimeKind: req.RuntimeKind,
			Protocol:    model.ManagedProtocol(req.Protocol),
			Tag:         tag,
			Remark:      strings.TrimSpace(req.Remark),
			Listen:      strings.TrimSpace(req.Listen),
			Port:        req.Port,
			Enable:      enable,
			Status:      model.EndpointApplying,
		}
		if revive {
			endpoint.Id = tombstone.Id
			endpoint.CreatedAt = tombstone.CreatedAt
		}
		if _, err := s.resolveDriver(endpoint); err != nil {
			return err
		}
		if err := ensureSingletonManagedEndpoint(tx, endpoint); err != nil {
			return err
		}
		if revive {
			if err := resetManagedEndpointTombstone(tx, endpoint.Id); err != nil {
				return err
			}
			if err := tx.Save(&endpoint).Error; err != nil {
				return err
			}
		} else if err := tx.Create(&endpoint).Error; err != nil {
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
	if err := s.installAndApplyOnCreate(ctx, &endpoint); err != nil {
		return nil, err
	}
	return ManagedEndpointService{}.Get(userId, fmt.Sprintf("managed-%d", endpoint.Id))
}

func resetManagedEndpointTombstone(tx *gorm.DB, endpointID int) error {
	var clientIDs []int
	if err := tx.Model(&model.ManagedEndpointClient{}).Where("endpoint_id = ?", endpointID).Pluck("id", &clientIDs).Error; err != nil {
		return err
	}
	if len(clientIDs) > 0 {
		if err := tx.Where("owner_type = ? AND owner_id IN ?", "managed_endpoint_client", clientIDs).Delete(&model.ManagedSecret{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Where("endpoint_id = ?", endpointID).Delete(&model.ManagedEndpointClient{}).Error; err != nil {
		return err
	}
	if err := tx.Where("endpoint_id = ?", endpointID).Delete(&model.ManagedEndpointClientTraffic{}).Error; err != nil {
		return err
	}
	return tx.Where("owner_type = ? AND owner_id = ?", "managed_endpoint", endpointID).Delete(&model.ManagedSecret{}).Error
}

func (s ManagedEndpointMutationService) Update(ctx context.Context, userId int, id string, req ManagedEndpointUpdateRequest) (*ManagedEndpointView, error) {
	if err := req.normalizeConfig(); err != nil {
		return nil, err
	}
	if req.Mieru != nil && strings.TrimSpace(req.Mieru.Host) != "" {
		host, err := normalizeManagedMieruHost(req.Mieru.Host)
		if err != nil {
			return nil, err
		}
		req.Listen = &host
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
	neverApplied := false
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
		neverApplied = (endpoint.Status == model.EndpointFailed || endpoint.Status == model.EndpointRolledBack) && strings.TrimSpace(endpoint.LastAppliedHash) == "" && strings.TrimSpace(endpoint.LastObservedHash) == ""
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
	var runtimeErr error
	if !neverApplied {
		runtimeErr = s.callDriver(ctx, endpoint.RuntimeKind, func(d driver.Driver, inbound *model.Inbound) error {
			_, err := d.Delete(ctx, inbound)
			return err
		}, &endpoint)
	}
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
		p, err := s.resolveProvisioner(endpoint)
		if err != nil {
			return nil, nil, err
		}
		return nil, installPlanFromProvisioner(p.Plan(endpoint.RuntimeKind)), nil
	case "install", "update", "uninstall":
		reqHash := managedRequestHash("runtime."+action+":"+id, nil)
		replayed := false
		if idempotencyKey != "" {
			if existing, ok, err := findApplyLog(database.GetDB(), idempotencyKey, reqHash); err != nil || ok {
				if err != nil {
					return nil, nil, err
				}
				replayed = ok
				if existing.EndpointId != endpoint.Id {
					return nil, nil, ErrManagedIdempotencyConflict
				}
			}
		}
		if !replayed {
			if err := createApplyLog(database.GetDB(), idempotencyKey, endpoint.Id, "runtime."+action, reqHash, model.EndpointApplying, ""); err != nil {
				return nil, nil, err
			}
			p, err := s.resolveProvisioner(endpoint)
			if err != nil {
				return nil, nil, err
			}
			var res provisioner.Result
			var tx provisioner.Transaction
			if action == "install" || action == "update" {
				if tp, ok := p.(provisioner.TransactionalProvisioner); ok {
					if action == "install" {
						tx, err = tp.BeginInstall(ctx, endpoint.RuntimeKind)
					} else {
						tx, err = tp.BeginUpdate(ctx, endpoint.RuntimeKind)
					}
					if tx != nil {
						res = tx.Result()
					}
				} else if action == "install" {
					res, err = p.Install(ctx, endpoint.RuntimeKind)
				} else {
					res, err = p.Update(ctx, endpoint.RuntimeKind)
				}
			} else {
				if err := s.stopRuntimeBeforeUninstall(ctx, endpoint); err != nil {
					return nil, nil, err
				}
				res, err = p.Uninstall(ctx, endpoint.RuntimeKind)
			}
			status := model.EndpointActive
			if action == "uninstall" {
				status = model.EndpointDisabled
			}
			code := ""
			if err != nil {
				status = model.EndpointFailed
				code = safeManagedErrorCode(err)
			} else if res.RolledBack {
				status = model.EndpointRolledBack
			}
			if err := database.GetDB().Model(&model.ManagedEndpoint{}).Where("id = ?", endpoint.Id).Updates(map[string]any{"status": status, "last_error": code}).Error; err != nil {
				return nil, nil, err
			}
			if err := database.GetDB().Model(&model.ManagedEndpointApplyLog{}).Where("endpoint_id = ? AND action = ?", endpoint.Id, "runtime."+action).Order("id desc").Limit(1).Updates(map[string]any{"status": status, "error": code}).Error; err != nil {
				return nil, nil, err
			}
			if err != nil {
				return nil, nil, err
			}
			if action != "uninstall" {
				if err := s.applyEndpoint(ctx, &endpoint, action); err != nil {
					if tx != nil {
						res, _ = tx.Rollback(ctx)
						code = safeManagedErrorCode(err)
						_ = database.GetDB().Model(&model.ManagedEndpoint{}).Where("id = ?", endpoint.Id).Updates(map[string]any{"status": model.EndpointRolledBack, "last_error": code}).Error
						_ = database.GetDB().Model(&model.ManagedEndpointApplyLog{}).Where("endpoint_id = ? AND action = ?", endpoint.Id, "runtime."+action).Order("id desc").Limit(1).Updates(map[string]any{"status": model.EndpointRolledBack, "error": code}).Error
						_ = res
					}
					return nil, nil, err
				}
				if tx != nil {
					if err := tx.Commit(ctx); err != nil {
						return nil, nil, err
					}
				}
			}
		}
	default:
		return nil, nil, fmt.Errorf("unsupported action %q", action)
	}
	view, err := ManagedEndpointService{}.Get(userId, id)
	return view, nil, err
}

func (s ManagedEndpointMutationService) stopRuntimeBeforeUninstall(ctx context.Context, endpoint model.ManagedEndpoint) error {
	d, err := s.resolveDriver(endpoint)
	if err != nil {
		return err
	}
	stopper, ok := d.(driver.Stopper)
	if !ok {
		return nil
	}
	inbound, err := s.inboundFromDurable(endpoint)
	if err != nil {
		return err
	}
	return stopper.Stop(ctx, inbound)
}

func (s ManagedEndpointMutationService) CreateClient(ctx context.Context, userId int, endpointRef string, req ManagedEndpointClientCreateRequest) (model.ManagedEndpointClient, error) {
	if strings.TrimSpace(req.SubID) == "" {
		return model.ManagedEndpointClient{}, errors.New("subId is required")
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
		record, err := resolveManagedSubscriptionClient(tx, req.SubID)
		if err != nil {
			return err
		}
		req.Email = record.Email
		if existing, ok, err := findApplyLog(tx, req.IdempotencyKey, reqHash); err != nil || ok {
			if ok {
				replayed = true
				return tx.First(&client, "endpoint_id = ? AND client_id = ?", existing.EndpointId, record.Id).Error
			}
			return err
		}
		client = model.ManagedEndpointClient{EndpointId: endpoint.Id, ClientId: record.Id, SubID: record.SubID, Email: record.Email, Enable: enable, State: model.EndpointClientPending, Status: model.EndpointClientPending}
		if req.ClientID != "" {
			client.PublicIdentity = strings.TrimSpace(req.ClientID)
		} else {
			client.PublicIdentity = uuid.NewString()
		}
		if req.Address != "" {
			client.Address = strings.TrimSpace(req.Address)
		} else if endpoint.RuntimeKind == model.RuntimeAmneziaWG {
			addr, err := allocateAWGIPv4(tx, endpoint)
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
		client.Status = client.State
		_ = hydrateManagedClientSubscription(database.GetDB(), &client)
		return client, nil
	}
	if err := s.applyEndpoint(ctx, &endpoint, "client.create"); err != nil {
		return model.ManagedEndpointClient{}, err
	}
	return loadManagedEndpointClient(database.GetDB(), endpoint.Id, client.Id)
}

func (s ManagedEndpointMutationService) ListClients(userId, endpointID int) ([]model.ManagedEndpointClient, error) {
	var endpoint model.ManagedEndpoint
	if err := database.GetDB().First(&endpoint, "id = ? AND user_id = ? AND status <> ?", endpointID, userId, model.EndpointDeleted).Error; err != nil {
		return nil, err
	}
	var rows []model.ManagedEndpointClient
	db := database.GetDB()
	err := db.Table("managed_endpoint_clients AS mec").
		Select("mec.id, mec.endpoint_id, mec.client_id, mec.email, mec.enable, mec.state, mec.public_identity, mec.address, mec.credential_ref, mec.client_config, mec.observed_config, mec.last_applied_hash, mec.last_error, mec.created_at, mec.updated_at").
		Where("mec.endpoint_id = ? AND mec.state <> ?", endpointID, model.EndpointClientDeleted).
		Order("mec.id asc").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if err := hydrateManagedClientSubscription(db, &rows[i]); err != nil {
			return nil, err
		}
	}
	return rows, nil
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
		if req.SubID != nil {
			record, err := resolveManagedSubscriptionClient(tx, *req.SubID)
			if err != nil {
				return err
			}
			client.ClientId = record.Id
			client.Email = record.Email
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
		client.Status = client.State
		_ = hydrateManagedClientSubscription(database.GetDB(), &client)
		return client, nil
	}
	if err := s.applyEndpoint(ctx, &endpoint, "client.create"); err != nil {
		return model.ManagedEndpointClient{}, err
	}
	return loadManagedEndpointClient(database.GetDB(), endpoint.Id, client.Id)
}

func loadManagedEndpointClient(db *gorm.DB, endpointID, clientID int) (model.ManagedEndpointClient, error) {
	var client model.ManagedEndpointClient
	if err := db.First(&client, "id = ? AND endpoint_id = ?", clientID, endpointID).Error; err != nil {
		return client, err
	}
	client.Status = client.State
	if err := hydrateManagedClientSubscription(db, &client); err != nil {
		return client, err
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

func (s ManagedEndpointMutationService) ClientExport(userId int, endpointRef string, clientID int) (ManagedClientExportResponse, error) {
	endpointID, err := nativeManagedID(endpointRef)
	if err != nil {
		return ManagedClientExportResponse{}, err
	}
	var endpoint model.ManagedEndpoint
	if err := database.GetDB().First(&endpoint, "id = ? AND user_id = ?", endpointID, userId).Error; err != nil {
		return ManagedClientExportResponse{}, err
	}
	var client model.ManagedEndpointClient
	if err := database.GetDB().First(&client, "id = ? AND endpoint_id = ?", clientID, endpoint.Id).Error; err != nil {
		return ManagedClientExportResponse{}, err
	}
	if err := hydrateManagedClientSubscription(database.GetDB(), &client); err != nil {
		return ManagedClientExportResponse{}, err
	}
	out := ManagedClientExportResponse{Filename: strings.TrimSpace(endpoint.Tag) + "-" + strings.TrimSpace(client.SubID) + ".txt", Subscriptions: managedSubscriptionURLs(client.SubID)}
	switch endpoint.RuntimeKind {
	case model.RuntimeAmneziaWG:
		var cfg awg.DesiredConfig
		if err := json.Unmarshal([]byte(endpoint.DesiredConfig), &cfg); err != nil {
			return ManagedClientExportResponse{}, err
		}
		clients, err := s.awgClientsFromDB(database.GetDB(), endpoint.Id, cfg.ClientDefaults)
		if err != nil {
			return ManagedClientExportResponse{}, err
		}
		for _, c := range clients {
			if c.ID == client.PublicIdentity {
				content, err := awg.RenderClientConfig(cfg.Server, c)
				if err != nil {
					return ManagedClientExportResponse{}, err
				}
				out.Content = content
				return out, nil
			}
		}
	case model.RuntimeMieru:
		rows, err := newestManagedSecrets(database.GetDB(), "managed_endpoint_client", client.Id, "password")
		if err != nil {
			return ManagedClientExportResponse{}, err
		}
		password, err := s.decryptSecret(rows["password"])
		if err != nil {
			return ManagedClientExportResponse{}, err
		}
		var cfg mieru.ServerConfig
		if err := json.Unmarshal([]byte(endpoint.DesiredConfig), &cfg); err != nil {
			return ManagedClientExportResponse{}, err
		}
		host, err := normalizeManagedMieruHost(endpoint.Listen)
		if err != nil {
			return ManagedClientExportResponse{}, err
		}
		content, err := mieru.ClientJSON(mieru.ClientExport{ProfileName: endpoint.Remark, UserName: client.PublicIdentity, Password: string(password), Endpoints: []mieru.Endpoint{{Host: host, PortBinding: cfg.PortBindings}}, MTU: cfg.MTU})
		if err != nil {
			return ManagedClientExportResponse{}, err
		}
		out.Filename = strings.TrimSpace(endpoint.Tag) + "-" + strings.TrimSpace(client.SubID) + "-mieru.json"
		out.Content = string(content)
		return out, nil
	case model.RuntimeNaiveProxy:
		rows, err := newestManagedSecrets(database.GetDB(), "managed_endpoint_client", client.Id, "password")
		if err != nil {
			return ManagedClientExportResponse{}, err
		}
		password, err := s.decryptSecret(rows["password"])
		if err != nil {
			return ManagedClientExportResponse{}, err
		}
		var payload struct {
			Endpoint naiveproxy.Endpoint `json:"endpoint"`
		}
		if err := json.Unmarshal([]byte(endpoint.DesiredConfig), &payload); err != nil {
			return ManagedClientExportResponse{}, err
		}
		uri, err := (naiveproxy.User{ID: client.PublicIdentity, Username: client.PublicIdentity, Password: string(password), Enabled: client.Enable}).ExportURI(payload.Endpoint)
		if err != nil {
			return ManagedClientExportResponse{}, err
		}
		out.Content = uri
		return out, nil
	}
	return ManagedClientExportResponse{}, fmt.Errorf("client export unavailable")
}

func managedSubscriptionURLs(subID string) ManagedClientExportSubscriptions {
	if strings.TrimSpace(subID) == "" {
		return ManagedClientExportSubscriptions{}
	}
	settings := SettingService{}
	rawEnable, _ := settings.GetSubEnable()
	jsonEnable, _ := settings.GetSubJsonEnable()
	clashEnable, _ := settings.GetSubClashEnable()
	rawURI, _ := settings.GetSubURI()
	jsonURI, _ := settings.GetSubJsonURI()
	clashURI, _ := settings.GetSubClashURI()
	subPath, _ := settings.GetSubPath()
	jsonPath, _ := settings.GetSubJsonPath()
	clashPath, _ := settings.GetSubClashPath()
	base := ""
	if d, _ := settings.GetSubDomain(); d != "" {
		base = settings.BuildSubURIBase(d)
	} else if d, _ := settings.GetWebDomain(); d != "" {
		base = settings.BuildSubURIBase(d)
	}
	out := ManagedClientExportSubscriptions{}
	if rawEnable {
		out.Raw = buildManagedSubscriptionURL(rawURI, base, subPath, subID)
	}
	if jsonEnable {
		out.JSON = buildManagedSubscriptionURL(jsonURI, base, jsonPath, subID)
	}
	if clashEnable {
		out.Clash = buildManagedSubscriptionURL(clashURI, base, clashPath, subID)
	}
	return out
}

func buildManagedSubscriptionURL(configured, base, path, subID string) string {
	if configured != "" {
		if !strings.HasSuffix(configured, "/") {
			configured += "/"
		}
		return configured + url.PathEscape(subID)
	}
	if base == "" {
		return ""
	}
	if path == "" {
		path = "/"
	}
	return strings.TrimRight(base, "/") + "/" + strings.Trim(path, "/") + "/" + url.PathEscape(subID)
}

func resolveManagedSubscriptionClient(tx *gorm.DB, subID string) (model.ClientRecord, error) {
	subID = strings.TrimSpace(subID)
	if subID == "" {
		return model.ClientRecord{}, errors.New("subId is required")
	}
	var rows []model.ClientRecord
	if err := tx.Where("sub_id = ? AND enable = ?", subID, true).Limit(2).Find(&rows).Error; err != nil {
		return model.ClientRecord{}, err
	}
	if len(rows) != 1 {
		return model.ClientRecord{}, errors.New("subscription client binding must resolve exactly one enabled client")
	}
	return rows[0], nil
}

func hydrateManagedClientSubscription(tx *gorm.DB, client *model.ManagedEndpointClient) error {
	if client == nil || client.ClientId == 0 {
		return nil
	}
	var rec model.ClientRecord
	if err := tx.Select("id", "sub_id", "email").First(&rec, "id = ?", client.ClientId).Error; err != nil {
		return err
	}
	client.SubID = rec.SubID
	client.Email = rec.Email
	client.Status = client.State
	return nil
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
	return installPlanFromProvisioner(provisioner.NewLocal(provisioner.Config{}).Plan(kind))
}

func (ManagedEndpointService) InstallPlans() []InstallPlan {
	kinds := provisioner.Kinds()
	out := make([]InstallPlan, 0, len(kinds))
	for _, kind := range kinds {
		out = append(out, ManagedEndpointService{}.InstallPlan(kind))
	}
	return out
}

func installPlanFromProvisioner(plan provisioner.Plan) InstallPlan {
	out := InstallPlan{
		RuntimeKind:         plan.RuntimeKind,
		Supported:           plan.Supported,
		Blocked:             plan.Blocked,
		RequiresPinnedImage: plan.RequiresPinnedImage,
		ImageRef:            plan.ArtifactRef,
		ArtifactRef:         plan.ArtifactRef,
		Version:             plan.Version,
		Reason:              plan.Reason,
		Capabilities:        plan.Capabilities,
	}
	switch plan.RuntimeKind {
	case model.RuntimeAmneziaWG:
		out.BackendProfiles = []InstallPlanBackendProfile{{Kind: "docker-amnezia-awg2", ContainerName: provisioner.AWG2ContainerName, HostConfigDir: provisioner.AWG2HostConfigPath, ContainerConfigDir: provisioner.AWG2GuestConfigPath}}
	case model.RuntimeNaiveProxy:
		out.BackendProfiles = []InstallPlanBackendProfile{{Kind: "docker-naiveproxy", ContainerName: provisioner.NaiveContainerName, HostConfigDir: provisioner.NaiveHostConfigPath, ContainerConfigDir: provisioner.NaiveGuestConfig}}
	case model.RuntimeMieru:
		out.BackendProfiles = []InstallPlanBackendProfile{{Kind: "native-mita", HostConfigDir: "/usr/local/bin"}}
	}
	return out
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

func normalizeManagedMieruHost(raw string) (string, error) {
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
	if addr, err := netip.ParseAddr(host); err == nil {
		if !addr.Is4() {
			return "", errors.New("mieru public host must be IPv4 or DNS name")
		}
		return host, nil
	}
	if host == "" || len(host) > 253 || strings.ContainsAny(host, ":/\\\x00\r\n\t ") {
		return "", errors.New("invalid mieru public host")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("invalid mieru public host")
		}
		for _, r := range label {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				continue
			}
			return "", errors.New("invalid mieru public host")
		}
	}
	return host, nil
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
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	var obj struct {
		ManagedProtocols []string `json:"managedProtocols"`
	}
	if err := json.Unmarshal([]byte(raw), &obj); err == nil && obj.ManagedProtocols != nil {
		for _, cap := range obj.ManagedProtocols {
			if model.RuntimeKind(strings.TrimSpace(cap)) == kind {
				return true
			}
		}
		return false
	}
	var caps []string
	if err := json.Unmarshal([]byte(raw), &caps); err == nil {
		for _, cap := range caps {
			if model.RuntimeKind(strings.TrimSpace(cap)) == kind {
				return true
			}
		}
		return false
	}
	if strings.ContainsAny(raw, "{}[]\":") {
		return false
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

func (s ManagedEndpointMutationService) resolveProvisioner(endpoint model.ManagedEndpoint) (provisioner.Provisioner, error) {
	if s.Drivers == nil {
		return nil, fmt.Errorf("%w: managed runtime provider unavailable", driver.ErrUnsupportedRuntime)
	}
	return s.Drivers.ProvisionerForEndpoint(endpoint)
}

func ensureSingletonManagedEndpoint(tx *gorm.DB, endpoint model.ManagedEndpoint) error {
	switch endpoint.RuntimeKind {
	case model.RuntimeAmneziaWG, model.RuntimeMieru, model.RuntimeNaiveProxy:
	default:
		return nil
	}
	var count int64
	q := tx.Model(&model.ManagedEndpoint{}).
		Where("runtime_kind = ? AND status <> ?", endpoint.RuntimeKind, model.EndpointDeleted)
	if endpoint.NodeID == nil {
		q = q.Where("node_id IS NULL")
	} else {
		q = q.Where("node_id = ?", *endpoint.NodeID)
	}
	if endpoint.Id > 0 {
		q = q.Where("id <> ?", endpoint.Id)
	}
	if err := q.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("managed runtime %s already has an active endpoint on this node", endpoint.RuntimeKind)
	}
	return nil
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
	interfaceName := strings.TrimSpace(req.InterfaceName)
	if interfaceName == "" {
		interfaceName = "awg0"
	}
	if req.ListenPort != 0 && req.ListenPort != endpoint.Port {
		return "", nil, errors.New("amneziawg listenPort must match endpoint port")
	}
	server := awg.DefaultServer(interfaceName, endpoint.Port)
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
	if req.Jc != 0 {
		server.Obfuscation20 = awg.Obfuscation20{
			Jc: req.Jc, Jmin: req.Jmin, Jmax: req.Jmax,
			S1: req.S1, S2: req.S2, S3: req.S3, S4: req.S4,
			H1: strings.TrimSpace(req.H1), H2: strings.TrimSpace(req.H2),
			H3: strings.TrimSpace(req.H3), H4: strings.TrimSpace(req.H4),
		}
	}
	privateKey := strings.TrimSpace(req.ServerPrivateKey)
	publicKey := strings.TrimSpace(req.ServerPublicKey)
	if privateKey == "" {
		var err error
		privateKey, publicKey, err = wgutil.GenerateWireguardKeypair()
		if err != nil {
			return "", nil, err
		}
	}
	if publicKey == "" {
		var err error
		publicKey, err = wgutil.PublicKeyFromPrivate(privateKey)
		if err != nil {
			return "", nil, err
		}
	}
	server.PrivateKey = privateKey
	server.PublicKey = publicKey
	if err := awg.ValidateServer(server); err != nil {
		return "", nil, err
	}
	clientAllowedIPs := strings.TrimSpace(req.ClientAllowedIPs)
	if clientAllowedIPs == "" {
		clientAllowedIPs = "0.0.0.0/0"
	}
	keepalive := req.PersistentKeepalive
	if keepalive == 0 {
		keepalive = 25
	}
	redacted := server
	redacted.PrivateKey = secretRef("managed_endpoint", endpoint.Id, "server.privateKey")
	redacted.PublicKey = publicKey
	raw, err := json.Marshal(awg.DesiredConfig{
		Server: redacted,
		ClientDefaults: awg.ClientDefaults{
			AllowedIPs: clientAllowedIPs, PersistentKeepalive: keepalive,
		},
	})
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
	bindings := make([]mieru.PortBinding, 0, len(req.PortBindings))
	for _, input := range req.PortBindings {
		transport := mieru.Transport(strings.ToUpper(strings.TrimSpace(input.Protocol)))
		if transport == "" {
			transport = mieru.TransportTCP
		}
		bindings = append(bindings, mieru.PortBinding{
			Port: input.Port, Protocol: transport, PortRange: strings.TrimSpace(input.PortRange),
		})
	}
	if len(bindings) == 0 {
		transport := mieru.TransportTCP
		if strings.EqualFold(req.Transport, string(mieru.TransportUDP)) {
			transport = mieru.TransportUDP
		}
		bindings = []mieru.PortBinding{{Port: endpoint.Port, Protocol: transport}}
	}
	bootstrapPassword := randomPassword()
	cfg := mieru.ServerConfig{PortBindings: bindings, MTU: req.MTU, Users: []mieru.User{{Name: managedBootstrapIdentity(endpoint.Id), Password: bootstrapPassword}}}
	if err := cfg.ValidateFull(); err != nil {
		return "", nil, err
	}
	raw, err := json.Marshal(mieru.RedactConfig(cfg))
	if err != nil {
		return "", nil, err
	}
	secret, err := s.encryptSecret("managed_endpoint", endpoint.Id, "bootstrap.password", []byte(bootstrapPassword))
	return string(raw), []model.ManagedSecret{secret}, err
}

func (s ManagedEndpointMutationService) buildNaiveDesired(endpoint model.ManagedEndpoint, req *ManagedNaiveProxyConfig) (string, []model.ManagedSecret, error) {
	if req == nil {
		return "", nil, errors.New("naiveproxy config is required")
	}
	domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(req.Domain)), ".")
	sni := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(req.SNI)), ".")
	if sni != "" && sni != domain {
		return "", nil, errors.New("naiveproxy sni must match domain")
	}
	if req.Port != 0 && req.Port != endpoint.Port {
		return "", nil, errors.New("naiveproxy config port must match endpoint port")
	}
	tlsMode := strings.ToLower(strings.TrimSpace(req.TLSMode))
	if tlsMode != "" && tlsMode != "acme" {
		return "", nil, errors.New("naiveproxy managed TLS is not supported; use acme")
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
	bootstrapPassword := randomPassword()
	server := naiveproxy.Server{Endpoint: naiveproxy.Endpoint{Domain: domain, ListenIP: listenIP, Port: endpoint.Port, ACMEEmail: strings.TrimSpace(req.ACMEEmail)}, Users: []naiveproxy.User{{ID: managedBootstrapIdentity(endpoint.Id), Username: managedBootstrapIdentity(endpoint.Id), Password: bootstrapPassword, Enabled: true}}}
	if err := server.Validate(); err != nil {
		return "", nil, err
	}
	raw, err := json.Marshal(map[string]any{"endpoint": server.Endpoint, "users": []any{}})
	if err != nil {
		return "", nil, err
	}
	secret, err := s.encryptSecret("managed_endpoint", endpoint.Id, "bootstrap.password", []byte(bootstrapPassword))
	return string(raw), []model.ManagedSecret{secret}, err
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
		var generation int
		if err := tx.Model(&model.ManagedSecret{}).
			Where("owner_type = ? AND owner_id = ? AND secret_kind = ?", secret.OwnerType, secret.OwnerId, secret.SecretKind).
			Select("COALESCE(MAX(generation), 0)").
			Scan(&generation).Error; err != nil {
			return err
		}
		secret.Generation = generation + 1
		if err := tx.Create(&secret).Error; err != nil {
			if !isManagedSecretLegacyUniqueError(err) {
				return err
			}
			if err := tx.Model(&model.ManagedSecret{}).
				Where("owner_type = ? AND owner_id = ? AND secret_kind = ?", secret.OwnerType, secret.OwnerId, secret.SecretKind).
				Updates(map[string]any{
					"generation":       secret.Generation,
					"envelope_version": secret.EnvelopeVersion,
					"key_id":           secret.KeyID,
					"nonce":            secret.Nonce,
					"ciphertext":       secret.Ciphertext,
					"fingerprint":      secret.Fingerprint,
					"updated_at":       secret.UpdatedAt,
				}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func isManagedSecretLegacyUniqueError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unique") && strings.Contains(text, "managed_secrets")
}

func newestManagedSecrets(tx *gorm.DB, ownerType string, ownerID int, kinds ...string) (map[string]model.ManagedSecret, error) {
	var rows []model.ManagedSecret
	q := tx.Where("owner_type = ? AND owner_id = ?", ownerType, ownerID)
	if len(kinds) > 0 {
		q = q.Where("secret_kind IN ?", kinds)
	}
	if err := q.Order("secret_kind ASC, generation DESC, created_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]model.ManagedSecret, len(kinds))
	for _, row := range rows {
		if _, seen := out[row.SecretKind]; seen {
			continue
		}
		out[row.SecretKind] = row
	}
	for _, kind := range kinds {
		row, ok := out[kind]
		if !ok || len(row.Ciphertext) == 0 {
			return nil, fmt.Errorf("%w: %s for %s/%d", ErrManagedSecretMissing, kind, ownerType, ownerID)
		}
	}
	return out, nil
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

func (s ManagedEndpointMutationService) installAndApplyOnCreate(ctx context.Context, endpoint *model.ManagedEndpoint) error {
	if endpoint.RuntimeKind != model.RuntimeAmneziaWG && endpoint.RuntimeKind != model.RuntimeMieru && endpoint.RuntimeKind != model.RuntimeNaiveProxy {
		return s.applyEndpoint(ctx, endpoint, "create")
	}
	p, err := s.resolveProvisioner(*endpoint)
	if err != nil {
		return err
	}
	plan := p.Plan(endpoint.RuntimeKind)
	if !plan.Supported || plan.Blocked {
		reason := strings.TrimSpace(plan.Reason)
		if reason == "" {
			reason = "runtime provisioning is unavailable"
		}
		err := fmt.Errorf("managed runtime install blocked: %s", reason)
		code := safeManagedErrorCode(err)
		_ = database.GetDB().Model(&model.ManagedEndpoint{}).Where("id = ?", endpoint.Id).Updates(map[string]any{"status": model.EndpointFailed, "last_error": code}).Error
		return err
	}
	var tx provisioner.Transaction
	if tp, ok := p.(provisioner.TransactionalProvisioner); ok {
		tx, err = tp.BeginInstall(ctx, endpoint.RuntimeKind)
	} else {
		_, err = p.Install(ctx, endpoint.RuntimeKind)
	}
	if err != nil {
		code := safeManagedErrorCode(err)
		_ = database.GetDB().Model(&model.ManagedEndpoint{}).Where("id = ?", endpoint.Id).Updates(map[string]any{"status": model.EndpointFailed, "last_error": code}).Error
		return err
	}
	if err := s.applyEndpoint(ctx, endpoint, "create"); err != nil {
		if tx != nil {
			_, _ = tx.Rollback(ctx)
		} else {
			_, _ = p.Uninstall(ctx, endpoint.RuntimeKind)
		}
		code := safeManagedErrorCode(err)
		_ = database.GetDB().Model(&model.ManagedEndpoint{}).Where("id = ?", endpoint.Id).Updates(map[string]any{"status": model.EndpointRolledBack, "last_error": code}).Error
		return err
	}
	if tx != nil {
		if err := tx.Commit(ctx); err != nil {
			code := safeManagedErrorCode(err)
			_ = database.GetDB().Model(&model.ManagedEndpoint{}).Where("id = ?", endpoint.Id).Updates(map[string]any{"status": model.EndpointFailed, "last_error": code}).Error
			return err
		}
	}
	return nil
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
		secrets, err := newestManagedSecrets(database.GetDB(), "managed_endpoint", endpoint.Id, "server.privateKey")
		if err != nil {
			return nil, err
		}
		plaintext, err := s.decryptSecret(secrets["server.privateKey"])
		if err != nil {
			return nil, err
		}
		cfg.Server.PrivateKey = string(plaintext)
		clients, err := s.awgClientsFromDB(database.GetDB(), endpoint.Id, cfg.ClientDefaults)
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
			if len(rows) == 0 {
				password, err := s.endpointBootstrapPassword(endpoint.Id)
				if err != nil {
					return nil, err
				}
				cfg.Users = append(cfg.Users, mieru.User{Name: managedBootstrapIdentity(endpoint.Id), Password: password})
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
			if len(rows) == 0 {
				password, err := s.endpointBootstrapPassword(endpoint.Id)
				if err != nil {
					return nil, err
				}
				identity := managedBootstrapIdentity(endpoint.Id)
				users = append(users, naiveproxy.User{ID: identity, Username: identity, Password: password, Enabled: true})
			}
			raw, _ := json.Marshal(map[string]any{"endpoint": payload.Endpoint, "users": users})
			settings = string(raw)
		}
		return &model.Inbound{Id: endpoint.Id, Tag: endpoint.Tag, Remark: endpoint.Remark, Listen: endpoint.Listen, Port: endpoint.Port, Protocol: model.Protocol(endpoint.RuntimeKind), Enable: endpoint.Enable, Settings: settings}, nil
	default:
		return nil, fmt.Errorf("unsupported runtime kind %q", endpoint.RuntimeKind)
	}
}

func (s ManagedEndpointMutationService) endpointBootstrapPassword(endpointID int) (string, error) {
	rows, err := newestManagedSecrets(database.GetDB(), "managed_endpoint", endpointID, "bootstrap.password")
	if err != nil {
		if !errors.Is(err, ErrManagedSecretMissing) {
			return "", err
		}
		password := randomPassword()
		secret, encErr := s.encryptSecret("managed_endpoint", endpointID, "bootstrap.password", []byte(password))
		if encErr != nil {
			return "", encErr
		}
		if encErr := upsertManagedSecrets(database.GetDB(), []model.ManagedSecret{secret}); encErr != nil {
			return "", encErr
		}
		return password, nil
	}
	plaintext, err := s.decryptSecret(rows["bootstrap.password"])
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func (s ManagedEndpointMutationService) clientPassword(clientID int) (string, error) {
	rows, err := newestManagedSecrets(database.GetDB(), "managed_endpoint_client", clientID, "password")
	if err != nil {
		return "", err
	}
	plaintext, err := s.decryptSecret(rows["password"])
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
			privateKey, publicKey, err = wgutil.GenerateWireguardKeypair()
			if err != nil {
				return nil, err
			}
		}
		if publicKey == "" {
			publicKey, err = wgutil.PublicKeyFromPrivate(privateKey)
			if err != nil {
				return nil, err
			}
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
		clients, err := s.awgClientsFromDB(tx, endpoint.Id, cfg.ClientDefaults)
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
		var cfg mieru.ServerConfig
		if err := json.Unmarshal([]byte(endpoint.DesiredConfig), &cfg); err != nil {
			return "", err
		}
		cfg.Users = nil
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

func (s ManagedEndpointMutationService) awgClientsFromDB(tx *gorm.DB, endpointID int, defaults awg.ClientDefaults) ([]awg.Client, error) {
	if strings.TrimSpace(defaults.AllowedIPs) == "" {
		defaults.AllowedIPs = "0.0.0.0/0"
	}
	if defaults.PersistentKeepalive == 0 {
		defaults.PersistentKeepalive = 25
	}
	var rows []model.ManagedEndpointClient
	if err := tx.Where("endpoint_id = ? AND state <> ?", endpointID, model.EndpointClientDeleted).Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	clients := make([]awg.Client, 0, len(rows))
	for _, row := range rows {
		secretRows, err := newestManagedSecrets(tx, "managed_endpoint_client", row.Id, "privateKey", "publicKey", "presharedKey")
		if err != nil {
			return nil, err
		}
		secrets := map[string]string{}
		for _, secretRow := range secretRows {
			plaintext, err := s.decryptSecret(secretRow)
			if err != nil {
				return nil, err
			}
			if len(plaintext) == 0 {
				return nil, fmt.Errorf("managed secret %s is empty", secretRow.SecretKind)
			}
			secrets[secretRow.SecretKind] = string(plaintext)
		}
		clients = append(clients, awg.Client{ID: row.PublicIdentity, Email: row.Email, PrivateKey: secrets["privateKey"], PublicKey: secrets["publicKey"], PresharedKey: secrets["presharedKey"], IPv4Address: row.Address, AllowedIPs: row.Address, ClientAllowedIPs: defaults.AllowedIPs, PersistentKeepalive: defaults.PersistentKeepalive, Enable: row.Enable})
	}
	return clients, nil
}

func allocateAWGIPv4(tx *gorm.DB, endpoint model.ManagedEndpoint) (string, error) {
	var used []string
	if err := tx.Model(&model.ManagedEndpointClient{}).Where("endpoint_id = ?", endpoint.Id).Pluck("address", &used).Error; err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for _, addr := range used {
		seen[strings.TrimSpace(addr)] = true
	}
	var desired awg.DesiredConfig
	if err := json.Unmarshal([]byte(endpoint.DesiredConfig), &desired); err != nil {
		return "", err
	}
	prefix, err := netip.ParsePrefix(strings.TrimSpace(desired.Server.IPv4Pool))
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() > 30 {
		return "", errors.New("invalid amneziawg IPv4 pool")
	}
	prefix = prefix.Masked()
	serverPrefix, err := netip.ParsePrefix(strings.TrimSpace(desired.Server.IPv4Address))
	if err != nil || !serverPrefix.Addr().Is4() {
		return "", errors.New("invalid amneziawg server IPv4 address")
	}
	serverAddr := serverPrefix.Addr()
	addr := prefix.Addr().Next()
	for scanned := 0; scanned < 65536 && addr.IsValid() && prefix.Contains(addr); scanned++ {
		next := addr.Next()
		if !next.IsValid() || !prefix.Contains(next) {
			break // IPv4 broadcast address
		}
		cidr := addr.String() + "/32"
		if addr != serverAddr && !seen[cidr] {
			return cidr, nil
		}
		addr = next
	}
	return "", errors.New("amneziawg IPv4 pool exhausted")
}

func secretRef(ownerType string, ownerID int, kind string) string {
	if ownerID == 0 {
		return "managed-secret://" + ownerType + "/" + kind
	}
	return fmt.Sprintf("managed-secret://%s/%d/%s", ownerType, ownerID, kind)
}

func managedBootstrapIdentity(endpointID int) string {
	return fmt.Sprintf("bootstrap-%d", endpointID)
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
