package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestEmitZodUsesStrictObjectsOnlyForStrictSchemas(t *testing.T) {
	var out bytes.Buffer
	schemas := []Schema{
		{Name: "ManagedEndpointView", Fields: []Field{{JSONName: "id", Type: TypeRef{Kind: KindString}}}},
		{Name: "Inbound", Fields: []Field{{JSONName: "id", Type: TypeRef{Kind: KindInt}}}},
	}
	if err := emitZod(&out, schemas, nil); err != nil {
		t.Fatalf("emitZod: %v", err)
	}
	generated := out.String()
	if !strings.Contains(generated, "export const ManagedEndpointViewSchema = z.object({\n  id: z.string(),\n}).strict();") {
		t.Fatalf("managed endpoint schema is not strict:\n%s", generated)
	}
	if strings.Contains(generated, "export const InboundSchema = z.object({\n  id: z.number().int(),\n}).strict();") {
		t.Fatalf("unrelated schema unexpectedly became strict:\n%s", generated)
	}
}
