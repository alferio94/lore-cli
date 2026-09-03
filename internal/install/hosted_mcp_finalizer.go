package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"reflect"
	"strings"

	"github.com/alferio94/lore-cli/internal/reconcile"
)

const hostedMCPFinalizerID = "mcp-finalizer"

type HostedMCPFinalizerCode string

const (
	CodeHostedMCPInvalidIntent HostedMCPFinalizerCode = "hosted_mcp_invalid_intent"
	CodeHostedMCPResolveFailed HostedMCPFinalizerCode = "hosted_mcp_resolve_failed"
	CodeHostedMCPRenderFailed  HostedMCPFinalizerCode = "hosted_mcp_render_failed"
	CodeHostedMCPWriteFailed   HostedMCPFinalizerCode = "hosted_mcp_write_failed"
)

func (c HostedMCPFinalizerCode) Error() string { return string(c) }

type hostedMCPFinalizerError struct {
	code    HostedMCPFinalizerCode
	path    string
	message string
	causes  []error
}

func (e *hostedMCPFinalizerError) Error() string                { return e.message }
func (e *hostedMCPFinalizerError) Code() HostedMCPFinalizerCode { return e.code }
func (e *hostedMCPFinalizerError) Path() string                 { return e.path }
func (e *hostedMCPFinalizerError) Unwrap() []error {
	return append([]error(nil), e.causes...)
}
func (e *hostedMCPFinalizerError) Is(target error) bool {
	code, ok := target.(HostedMCPFinalizerCode)
	return ok && code == e.code
}

func hostedMCPError(code HostedMCPFinalizerCode, causes ...error) error {
	path, message := "", ""
	switch code {
	case CodeHostedMCPInvalidIntent:
		path, message = "hosted_mcp.intent", "hosted MCP intent is invalid"
	case CodeHostedMCPResolveFailed:
		path, message = "hosted_mcp.credential", "hosted MCP credential resolution failed"
	case CodeHostedMCPRenderFailed:
		path, message = "hosted_mcp.config", "hosted MCP configuration render failed"
	case CodeHostedMCPWriteFailed:
		path, message = "hosted_mcp.write", "hosted MCP protected write failed"
	}
	filtered := make([]error, 0, len(causes))
	for _, cause := range causes {
		if cause != nil {
			filtered = append(filtered, cause)
		}
	}
	return &hostedMCPFinalizerError{code: code, path: path, message: message, causes: filtered}
}

type hostedMCPCredentialResolver interface {
	Resolve(provider, slot string) ([]byte, error)
}

type hostedMCPRenderer interface {
	Render(target TargetID, endpoint string, credential []byte) ([]byte, error)
}

type hostedMCPBackedResource interface {
	Write([]byte) error
	Rollback() error
}

type hostedMCPSealedIntent struct {
	resource string
	target   TargetID
	endpoint string
	provider string
	slot     string
}

type hostedMCPIntentDocument struct {
	Endpoint   string                    `json:"endpoint"`
	Credential hostedMCPIntentCredential `json:"credential"`
}

type hostedMCPIntentCredential struct {
	Provider string `json:"provider"`
	Slot     string `json:"slot"`
}

func finalizeHostedMCP(plan TransactionPlan, input TransactionInput, journal *transactionFSJournal, resolver hostedMCPCredentialResolver, renderer hostedMCPRenderer) (*hostedMCPCompletionHandoff, error) {
	resealed, err := SealTransactionPlan(input)
	if err != nil || !reflect.DeepEqual(plan.DryRun(nil), resealed.DryRun(nil)) {
		return nil, rollbackHostedMCPJournal(journal, hostedMCPError(CodeHostedMCPInvalidIntent, err))
	}

	report := resealed.DryRun(nil)
	intent, err := sealedHostedMCPIntent(input, report)
	if err != nil || resolver == nil || renderer == nil {
		return nil, rollbackHostedMCPJournal(journal, hostedMCPError(CodeHostedMCPInvalidIntent, err))
	}
	resource, err := bindHostedMCPBackedResource(journal, intent.resource)
	if err != nil {
		return nil, rollbackHostedMCPJournal(journal, hostedMCPError(CodeHostedMCPInvalidIntent, err))
	}

	credential, err := resolver.Resolve(intent.provider, intent.slot)
	if err != nil || len(bytes.TrimSpace(credential)) == 0 {
		clear(credential)
		return nil, rollbackHostedMCP(resource, hostedMCPError(CodeHostedMCPResolveFailed, err))
	}
	defer clear(credential)

	configuration, err := renderer.Render(intent.target, intent.endpoint, credential)
	if err != nil || len(configuration) == 0 {
		clear(configuration)
		return nil, rollbackHostedMCP(resource, hostedMCPError(CodeHostedMCPRenderFailed, err))
	}
	defer clear(configuration)

	if err := resource.Write(configuration); err != nil {
		return nil, rollbackHostedMCP(resource, hostedMCPError(CodeHostedMCPWriteFailed, err))
	}
	handoff, err := newHostedMCPCompletionHandoff(plan, input, resealed, journal, intent.resource)
	if err != nil {
		return nil, rollbackHostedMCP(resource, hostedMCPError(CodeHostedMCPInvalidIntent, err))
	}
	return handoff, nil
}

func sealedHostedMCPIntent(input TransactionInput, report TransactionReport) (hostedMCPSealedIntent, error) {
	if !report.AllAdmitted || report.MutationCount != 0 || report.FinalizationCount != 1 || report.Target != TargetPi && report.Target != TargetOpenCode {
		return hostedMCPSealedIntent{}, errors.New("invalid admitted report")
	}

	facts := input.Semantic.TargetFacts()
	var resource *SemanticResourceFact
	for i := range facts.Resources {
		if len(facts.Resources[i].SensitiveReferences) == 0 {
			continue
		}
		if resource != nil || len(facts.Resources[i].SensitiveReferences) != 1 {
			return hostedMCPSealedIntent{}, errors.New("invalid finalization resource count")
		}
		candidate := facts.Resources[i]
		resource = &candidate
	}
	if resource == nil || resource.Component != ComponentLoreServerMCP || resource.Mode != reconcile.ModeReplace {
		return hostedMCPSealedIntent{}, errors.New("invalid finalization resource")
	}

	ref := resource.SensitiveReferences[0]
	if ref.FinalizerID != hostedMCPFinalizerID || ref.Provider == "" || ref.Slot == "" {
		return hostedMCPSealedIntent{}, errors.New("invalid finalization reference")
	}
	permit := input.Permit
	projections := permit.Projections()
	if permit.IRID() != input.IR.IRID() || len(projections) != 1 || projections[0].FinalizerID != ref.FinalizerID || projections[0].Credential.Provider != ref.Provider || projections[0].Credential.Slot != ref.Slot {
		return hostedMCPSealedIntent{}, errors.New("invalid finalization permit")
	}

	matchedDecision := false
	for _, decision := range report.Decisions {
		if decision.Resource == resource.Resource && decision.Mode == resource.Mode && decision.Outcome != reconcile.OutcomeConflict {
			matchedDecision = true
			break
		}
	}
	if !matchedDecision {
		return hostedMCPSealedIntent{}, errors.New("unbound finalization intent")
	}

	document, err := decodeHostedMCPIntent(resource.Desired)
	if err != nil || document.Credential.Provider != ref.Provider || document.Credential.Slot != ref.Slot || !validHostedMCPEndpoint(document.Endpoint) {
		return hostedMCPSealedIntent{}, errors.New("invalid finalization document")
	}
	return hostedMCPSealedIntent{resource: resource.Resource, target: report.Target, endpoint: document.Endpoint, provider: ref.Provider, slot: ref.Slot}, nil
}

func decodeHostedMCPIntent(data []byte) (hostedMCPIntentDocument, error) {
	var document hostedMCPIntentDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return hostedMCPIntentDocument{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return hostedMCPIntentDocument{}, errors.New("multiple intent documents")
	}
	canonical, err := json.Marshal(document)
	if err != nil || !bytes.Equal(data, canonical) {
		return hostedMCPIntentDocument{}, errors.New("non-canonical intent document")
	}
	return document, nil
}

func validHostedMCPEndpoint(endpoint string) bool {
	if endpoint == "" || strings.TrimSpace(endpoint) != endpoint || strings.ContainsAny(endpoint, "\x00\r\n") {
		return false
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || !parsed.IsAbs() || parsed.Opaque != "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	return strings.HasSuffix(parsed.EscapedPath(), "/v1/mcp")
}

type transactionFSHostedMCPBackedResource struct {
	journal *transactionFSJournal
	path    string
}

func bindHostedMCPBackedResource(journal *transactionFSJournal, path string) (hostedMCPBackedResource, error) {
	if journal == nil || journal.platform == nil || !journal.active || !journal.sealed || journal.mutated != len(journal.states) || !validTransactionRelativePath(path) || path != strings.TrimSpace(path) {
		return nil, errors.New("inactive backed resource")
	}
	matches := 0
	for _, state := range journal.states {
		if state.path == path {
			matches++
		}
	}
	if matches != 1 {
		return nil, errors.New("resource is outside the active journal")
	}
	return &transactionFSHostedMCPBackedResource{journal: journal, path: path}, nil
}

func (r *transactionFSHostedMCPBackedResource) Write(data []byte) error {
	if r == nil || r.journal == nil || !r.journal.active || !r.journal.sealed || len(data) == 0 {
		return errors.New("inactive backed resource")
	}
	if err := injectTransactionFS(r.journal.fail, "write", r.path); err != nil {
		return err
	}
	return r.journal.platform.write(r.path, data)
}

func (r *transactionFSHostedMCPBackedResource) Rollback() error {
	if r == nil || r.journal == nil {
		return errors.New("inactive backed resource")
	}
	return r.journal.Rollback()
}

func rollbackHostedMCP(resource hostedMCPBackedResource, primary error) error {
	if resource == nil {
		return primary
	}
	if err := resource.Rollback(); err != nil {
		return transactionFSResidualRiskError(primary, err)
	}
	return primary
}

func rollbackHostedMCPJournal(journal *transactionFSJournal, primary error) error {
	if journal == nil {
		return primary
	}
	if err := journal.Rollback(); err != nil {
		return transactionFSResidualRiskError(primary, err)
	}
	return primary
}

type hostedMCPNativeRenderer struct{}

func (hostedMCPNativeRenderer) Render(target TargetID, endpoint string, credential []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(credential)
	if !validHostedMCPEndpoint(endpoint) || len(trimmed) == 0 || bytes.ContainsAny(trimmed, "\r\n") {
		return nil, errors.New("invalid hosted MCP render input")
	}
	authorization := "Bearer " + string(trimmed)
	var payload any
	switch target {
	case TargetPi:
		payload = struct {
			MCPServers map[string]any `json:"mcpServers"`
		}{MCPServers: map[string]any{"lore": map[string]any{"url": endpoint, "headers": map[string]string{"Authorization": authorization}}}}
	case TargetOpenCode:
		payload = struct {
			MCP map[string]any `json:"mcp"`
		}{MCP: map[string]any{"lore": map[string]any{"type": "remote", "url": endpoint, "enabled": true, "headers": map[string]string{"Authorization": authorization}}}}
	default:
		return nil, errors.New("unsupported hosted MCP target")
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, errors.New("hosted MCP encoding failed")
	}
	return append(encoded, '\n'), nil
}
