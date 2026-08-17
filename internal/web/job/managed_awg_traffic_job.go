package job

import (
	"context"
	"sync"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// ManagedAWGTrafficJob collects official AWG peer counters for local and remote
// managed endpoints through the typed runtime driver contract.
type ManagedAWGTrafficJob struct{ mu sync.Mutex }

func NewManagedAWGTrafficJob() *ManagedAWGTrafficJob { return &ManagedAWGTrafficJob{} }

func (j *ManagedAWGTrafficJob) Run() {
	if !j.mu.TryLock() {
		return
	}
	defer j.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	collector := service.ManagedAWGTrafficCollector{Drivers: service.RuntimeManagerDriverProvider{}}
	if err := collector.Collect(ctx); err != nil {
		logger.Warning("managed AWG traffic collection failed: ", err)
	}
}
