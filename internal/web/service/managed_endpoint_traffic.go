package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/web/runtime/driver"

	"gorm.io/gorm"
)

// ManagedAWGTrafficCollector folds monotonic official AWG peer counters into the
// durable per-client accounting table. It never persists key material returned
// by the runtime; peer identity is the panel-generated public client ID.
type ManagedAWGTrafficCollector struct {
	Drivers ManagedDriverProvider
	Now     func() time.Time
}

type managedAWGObserved struct {
	LastHandshakeUnix int64 `json:"lastHandshakeUnix,omitempty"`
	RxBytes           int64 `json:"rxBytes"`
	TxBytes           int64 `json:"txBytes"`
}

func (c ManagedAWGTrafficCollector) Collect(ctx context.Context) error {
	db := database.GetDB()
	var endpoints []model.ManagedEndpoint
	if err := db.Where("runtime_kind = ? AND enable = ? AND status <> ?", model.RuntimeAmneziaWG, true, model.EndpointDeleted).Find(&endpoints).Error; err != nil {
		return err
	}
	mutation := ManagedEndpointMutationService{Drivers: c.Drivers}
	var errs []error
	for _, endpoint := range endpoints {
		d, err := mutation.resolveDriver(endpoint)
		if err != nil {
			errs = append(errs, fmt.Errorf("endpoint %d: %w", endpoint.Id, err))
			continue
		}
		observer, ok := d.(driver.PeerObserver)
		if !ok {
			continue
		}
		inbound, err := mutation.inboundFromDurable(endpoint)
		if err != nil {
			errs = append(errs, fmt.Errorf("endpoint %d: %w", endpoint.Id, err))
			continue
		}
		peers, err := observer.PeerStatuses(ctx, inbound)
		if err != nil {
			errs = append(errs, fmt.Errorf("endpoint %d: %w", endpoint.Id, err))
			continue
		}
		if err := c.storeEndpointSnapshot(db, endpoint, peers); err != nil {
			errs = append(errs, fmt.Errorf("endpoint %d: %w", endpoint.Id, err))
		}
	}
	return errors.Join(errs...)
}

func (c ManagedAWGTrafficCollector) storeEndpointSnapshot(db *gorm.DB, endpoint model.ManagedEndpoint, peers []driver.PeerStatusResult) error {
	var clients []model.ManagedEndpointClient
	if err := db.Where("endpoint_id = ? AND state <> ?", endpoint.Id, model.EndpointClientDeleted).Find(&clients).Error; err != nil {
		return err
	}
	byID := make(map[string]model.ManagedEndpointClient, len(clients))
	for _, client := range clients {
		byID[client.PublicIdentity] = client
	}
	nodeGUID := "local"
	if endpoint.NodeID != nil {
		var node model.Node
		if err := db.Select("guid").First(&node, *endpoint.NodeID).Error; err == nil && node.Guid != "" {
			nodeGUID = node.Guid
		} else {
			nodeGUID = fmt.Sprintf("node-%d", *endpoint.NodeID)
		}
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, peer := range peers {
			client, ok := byID[peer.ClientID]
			if !ok || peer.RxBytes < 0 || peer.TxBytes < 0 || peer.LastHandshakeUnix < 0 {
				continue
			}
			var row model.ManagedEndpointClientTraffic
			err := tx.Where("endpoint_id = ? AND email = ? AND node_guid = ?", endpoint.Id, client.Email, nodeGUID).First(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				row = model.ManagedEndpointClientTraffic{EndpointId: endpoint.Id, Email: client.Email, NodeGuid: nodeGUID}
			} else if err != nil {
				return err
			}
			row.Up += monotonicDelta(row.LastUpCounter, peer.RxBytes)
			row.Down += monotonicDelta(row.LastDownCounter, peer.TxBytes)
			row.LastUpCounter = peer.RxBytes
			row.LastDownCounter = peer.TxBytes
			row.LatestHandshake = peer.LastHandshakeUnix
			if peer.LastHandshakeUnix > 0 && now.Unix()-peer.LastHandshakeUnix <= 180 {
				row.LastOnline = peer.LastHandshakeUnix
			}
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
			observed, _ := json.Marshal(managedAWGObserved{LastHandshakeUnix: peer.LastHandshakeUnix, RxBytes: peer.RxBytes, TxBytes: peer.TxBytes})
			if err := tx.Model(&model.ManagedEndpointClient{}).Where("id = ?", client.Id).Updates(map[string]any{"observed_config": string(observed), "last_error": ""}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func monotonicDelta(previous, current int64) int64 {
	if current >= previous {
		return current - previous
	}
	return current
}
