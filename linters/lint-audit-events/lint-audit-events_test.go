package main

import (
	"fmt"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAuditEventDeclarationLinter(t *testing.T) {

	defaultFiles := map[string]string{
		"my-project/events/events.go": `package events // want package:"NewConnectionEvent"

// NewConnectionEvent is emitted when there is a new connection
var NewConnectionEvent string = "connection.new"

type Metadata struct {
  Name string
  Type string
}

type AuditEvent interface{
  GetType() string
}

func Emit(e AuditEvent) string {
  return e.GetType()
}

		    `,
		"my-project/goodimpl/goodimpl.go": `package goodimpl 

import "my-project/events"

type GoodAuditEventImplementation struct{
  Metadata events.Metadata
}

func (g GoodAuditEventImplementation) GetType() string{
  return g.Metadata.Type
}

func emitGoodAuditEventImplementation(){
    events.Emit(GoodAuditEventImplementation{
      Metadata: events.Metadata{
	Type: events.NewConnectionEvent,
      },
    })
}
		    `,
	}

	cases := []struct {
		description string
		// files must include the "want" comments expected by analysistest.
		// Tests will add a standard set of expected files (defaultFiles
		// above), so only include here that you want to be unique to a
		// test case.
		files map[string]string
	}{
		{
			description: "AuditEvent implementation with no Metadata field",
			files: map[string]string{

				"my-project/badimpl/badimpl.go": `package badimpl

type BadAuditEventImplementation struct{ // want "struct type my-project/badimpl.BadAuditEventImplementation implements AuditEvent but does not include the field Metadata of type my-project/events.Metadata"

  Type string
}

func (b BadAuditEventImplementation) GetType() string{
  return b.Type
}
`,

				"my-project/main.go": `package main

import (
  "my-project/badimpl"
  "my-project/events"
)

func main(){
    events.Emit(badimpl.BadAuditEventImplementation{
      Type: "bad audit event",
    })
}
`,
			},
		},
		{
			description: "Empty Metadata struct",
			files: map[string]string{
				"my-project/badmetadata/badmetadata.go": `package badmetadata

import (
  "my-project/events"
  "my-project/goodimpl"
)

func EmitAuditEvent(){
    events.Emit(goodimpl.GoodAuditEventImplementation{
        Metadata: events.Metadata{}, // want "required field Type is missing in a declaration of my-project/events.Metadata"
    })
}
`,
			},
		},
		{
			description: "Metadata with missing desired field",
			files: map[string]string{
				"my-project/badmetadata/badmetadata.go": `package badmetadata

import (
  "my-project/events"
  "my-project/goodimpl"
)

func EmitAuditEvent(){
  
    events.Emit(goodimpl.GoodAuditEventImplementation{
        Metadata: events.Metadata{ // want "required field Type is missing in a declaration of my-project/events.Metadata"
           Name: "My Metadata",
	},
    })
}
`,
			},
		},
		{
			description: "Metadata with empty string literal desired field",
			files: map[string]string{
				"my-project/badmetadata/badmetadata.go": `package badmetadata

import (
  "my-project/events"
  "my-project/goodimpl"
)

func EmitAuditEvent(){
  
    events.Emit(goodimpl.GoodAuditEventImplementation{
        Metadata: events.Metadata{ // want "the field Type in composite literal my-project/events.Metadata must have a value that is a variable or constant"
           Name: "My Metadata",
	   Type: "",
	},
    })
}
`,
			},
		},
		{
			description: "Metadata with nonempty string literal desired field",
			files: map[string]string{
				"my-project/badmetadata/badmetadata.go": `package badmetadata

import (
  "my-project/events"
  "my-project/goodimpl"
)

func EmitAuditEvent(){
  
    events.Emit(goodimpl.GoodAuditEventImplementation{
        Metadata: events.Metadata{ // want "the field Type in composite literal my-project/events.Metadata must have a value that is a variable or constant"
           Name: "My Metadata",
	   Type: "auditEventType",
	},
    })
}
`,
			},
		},
		{
			description: "Event type with no comment",
			files: map[string]string{
				"my-project/events/authnevent.go": `package events // want package:"NewConnectionEvent"
var AuthnEvent = "login.new" // want "my-project/events.AuthnEvent needs a comment since it is used when emitting an audit event"
			    `,
				"my-project/authn/authn.go": `
package authn 

import "my-project/events"

type AuthnEvent struct{
  Metadata events.Metadata
}

func (e AuthnEvent) GetType() string{
  return e.Metadata.Type
}

func emitAuthnEvent(){
    events.Emit(AuthnEvent{
      Metadata: events.Metadata{
	Type: events.AuthnEvent,
      },
    })
}
		    `,
			},
		},
		// TODO: Event type with comment that doesn't start with the
		// variable
		// TODO: Test case for Type value that isn't declared anywhere
		// TODO: Event type that clashes with an event type with the
		// same name in a different  package. Note that package facts
		// already store their package when you retrieve the fact via
		// AllPackageFacts, so there's no need to encode
		// this in the fact itself.
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {

			fmt.Println("STARTING TEST CASE: ", tc.description)
			defer fmt.Println("")

			// Assemble files for the test case by combining the default
			// files with the ones used for the test case into a new
			// map.
			m := make(map[string]string)
			for k, v := range tc.files {
				m[k] = v
			}
			for k, v := range defaultFiles {
				m[k] = v
			}
			dir, cleanup, err := analysistest.WriteFiles(m)

			defer cleanup()

			if err != nil {
				t.Fatalf("could not write test files: %v", err)
			}
			// For the GOCACHE variable
			cache := t.TempDir()

			fn, err := makeAuditEventDeclarationLinter(
				RequiredFieldInfo{
					workingDir:               dir,
					packageName:              "my-project/events",
					interfaceTypeName:        "AuditEvent",
					requiredFieldName:        "Metadata",
					requiredFieldPackageName: "my-project/events",
					requiredFieldTypeName:    "Metadata",
					envPairs: []string{
						"GOPATH=" + dir,
						"GO111MODULE=off",
						"GOCACHE=" + cache,
					},
					fieldTypeMustPopulateFields: []string{
						"Type",
					},
				})

			if err != nil {
				t.Fatal(err)
			}

			var f valueSpecFact

			var auditEventDeclarationLinter = &analysis.Analyzer{
				Name:      tc.description + ": lint-audit-event-declarations",
				Doc:       "ensure that Teleport audit events follow the structure required",
				Run:       fn,
				FactTypes: []analysis.Fact{&f},
			}

			analysistest.Run(
				t,
				dir,
				auditEventDeclarationLinter,
				"./...",
			)
		})
	}

}
