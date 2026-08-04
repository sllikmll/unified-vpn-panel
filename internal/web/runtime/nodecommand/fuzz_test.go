package nodecommand

import (
	"strings"
	"testing"
	"time"
)

func FuzzDecodeRequestStrict(f *testing.F) {
	f.Add(`{"version":"v1","supportedVersions":["v1"],"commandId":"cmd","idempotencyKey":"idem","nodeId":1,"targetGuid":"550e8400-e29b-41d4-a716-446655440000","endpointId":2,"runtimeKind":"wireguard","operation":"endpoint.status","desiredGeneration":1,"issuedAt":"1970-01-01T00:00:01Z","expiresAt":"1970-01-01T00:01:01Z"}`)
	f.Add(`{"version":"v1","supportedVersions":["v1"],"commandId":"cmd","idempotencyKey":"idem","nodeId":1,"targetGUID":"550e8400-e29b-41d4-a716-446655440000","endpointId":2,"runtimeKind":"wireguard","operation":"endpoint.status","desiredGeneration":1,"issuedAt":"1970-01-01T00:00:01Z","expiresAt":"1970-01-01T00:01:01Z"}`)
	f.Add(`{"version":"v1","version":"v1"}`)
	f.Add(`{"version":"v1","command":"sh"}`)
	f.Add(`{"version":"v1"} trailing`)

	now := time.Unix(2, 0).UTC()
	f.Fuzz(func(t *testing.T, input string) {
		req, err := DecodeRequest(strings.NewReader(input), DecodeOptions{
			MaxBytes: 4096,
			Now:      func() time.Time { return now },
		})
		if err == nil {
			if validateErr := req.Validate(now); validateErr != nil {
				t.Fatalf("decoded invalid request: %v", validateErr)
			}
			raw, marshalErr := req.MarshalJSON()
			if marshalErr != nil {
				t.Fatalf("marshal decoded request: %v", marshalErr)
			}
			if strings.Contains(string(raw), "private") || strings.Contains(string(raw), "stderr") {
				t.Fatalf("marshal leaked unsafe term: %s", raw)
			}
		}
	})
}
