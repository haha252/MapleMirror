package v2

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

func TestRegistryPriorityRange(t *testing.T) {
	for typ, spec := range RegisteredTypes() {
		if spec.Priority < PriorityHighest || spec.Priority > PriorityLowest {
			t.Fatalf("%s priority=%d outside range", typ, spec.Priority)
		}
	}
}

func TestKnownTypesRegistered(t *testing.T) {
	for _, typ := range []string{
		TypeSessionHello, TypeSessionWelcome, TypeProtocolError, TypeSyncCancel,
		TypeSyncResult, TypeSyncResultAck, TypeSyncAccepted, TypeSyncRejected,
		TypeSyncTask, TypeDownloadAuthorization, TypeDownloadAuthorizationAck,
		TypeAuthorizationStatus, TypeAuthorizationStatusAck, TypeTrafficEvent,
		TypeTrafficEventAck, TypePublicProbeChallenge, TypePublicProbeReady,
		TypeSwarmManifestReport, TypeSwarmManifestAck,
		TypeSwarmSourcesRequest, TypeSwarmSources, TypeSwarmAvailability,
		TypeInventorySnapshotSegment, TypeInventorySnapshotAck, TypeNodeStatus,
	} {
		if !Allowed(typ) {
			t.Fatalf("message type %s is not registered", typ)
		}
	}
}

// TestEveryDeclaredMessageTypeIsRegistered parses the protocol constants themselves,
// so adding a new Type* constant without assigning a transport priority fails CI.
func TestEveryDeclaredMessageTypeIsRegistered(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "message.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	declared := 0
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok {
			return true
		}
		for i, name := range spec.Names {
			if !strings.HasPrefix(name.Name, "Type") || i >= len(spec.Values) {
				continue
			}
			lit, ok := spec.Values[i].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Fatalf("%s must be an explicit string literal", name.Name)
			}
			typ, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("%s: %v", name.Name, err)
			}
			declared++
			if !Allowed(typ) {
				t.Errorf("declared message %s (%s) has no priority registry entry", name.Name, typ)
			}
		}
		return true
	})
	if declared != len(RegisteredTypes()) {
		t.Fatalf("declared message count=%d registry count=%d", declared, len(RegisteredTypes()))
	}
}
