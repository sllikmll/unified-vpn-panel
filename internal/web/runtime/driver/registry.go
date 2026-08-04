package driver

import (
	"fmt"
	"sort"
	"sync"

	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

type Registry struct {
	mu      sync.RWMutex
	drivers map[model.RuntimeKind]Driver
}

func NewRegistry() *Registry {
	return &Registry{drivers: make(map[model.RuntimeKind]Driver)}
}

func (r *Registry) Register(driver Driver) error {
	if isNilDriver(driver) {
		return ErrNilDriver
	}
	kind := driver.Kind()
	if kind == "" {
		return fmt.Errorf("%w: empty runtime kind", ErrUnsupportedRuntime)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.drivers == nil {
		r.drivers = make(map[model.RuntimeKind]Driver)
	}
	if _, exists := r.drivers[kind]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateRuntime, kind)
	}
	r.drivers[kind] = driver
	return nil
}

func (r *Registry) Driver(kind model.RuntimeKind) (Driver, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	driver, ok := r.drivers[kind]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedRuntime, kind)
	}
	return driver, nil
}

func (r *Registry) Kinds() []model.RuntimeKind {
	r.mu.RLock()
	defer r.mu.RUnlock()
	kinds := make([]model.RuntimeKind, 0, len(r.drivers))
	for kind := range r.drivers {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool {
		return kinds[i] < kinds[j]
	})
	return kinds
}

func isNilDriver(driver Driver) bool {
	return isNilInterface(driver)
}
