package job

import (
	"testing"
	"time"
)

func TestManagedAWGTrafficJobSkipsOverlappingRun(t *testing.T) {
	job := NewManagedAWGTrafficJob()
	job.mu.Lock()
	done := make(chan struct{})
	go func() {
		job.Run()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		job.mu.Unlock()
		t.Fatal("overlapping traffic job did not return immediately")
	}
	job.mu.Unlock()
}
