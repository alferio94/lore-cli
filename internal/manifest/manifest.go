// Package manifest provides a pure, strict codec for canonical manifest v3.
package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const SchemaV3 uint16 = 3

type ErrorCode string

func (code ErrorCode) Error() string { return string(code) }

const (
	CodeMalformedJSON     ErrorCode = "malformed_json"
	CodeRequiredField     ErrorCode = "required_field"
	CodeUnsupportedSchema ErrorCode = "unsupported_schema"
	CodeUnknownField      ErrorCode = "unknown_field"
	CodeDuplicateField    ErrorCode = "duplicate_field"
	CodeDuplicateRecord   ErrorCode = "duplicate_record"
	CodeInvalidField      ErrorCode = "invalid_field"
)

// CodecError identifies a non-secret contract violation. It deliberately does
// not retain input bytes, which keeps malformed secret-shaped JSON out of errors.
type CodecError struct {
	code ErrorCode
	path string
	msg  string
}

func (e *CodecError) Error() string   { return e.msg }
func (e *CodecError) Code() ErrorCode { return e.code }
func (e *CodecError) Path() string    { return e.path }
func (e *CodecError) Is(target error) bool {
	if code, ok := target.(ErrorCode); ok {
		return e.code == code
	}
	other, ok := target.(*CodecError)
	return ok && e.code == other.code
}

func codecError(code ErrorCode, path, message string) error {
	return &CodecError{code: code, path: path, msg: message}
}

type PackIdentity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
}

type ProfileIdentity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Scope   string `json:"scope"`
}

type Projection struct {
	Path      string `json:"path"`
	Ownership string `json:"ownership"`
	Hash      string `json:"hash"`
}

type RoleModelProvenance struct {
	Role     string `json:"role"`
	Model    string `json:"model"`
	Source   string `json:"source"`
	Location string `json:"location"`
}
type CapabilityProvenance struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}
type ReconciliationProvenance struct {
	Owner    string `json:"owner"`
	Decision string `json:"decision"`
}
type FinalizationReference struct {
	Path        string `json:"path"`
	FinalizerID string `json:"finalizer_id"`
	Provider    string `json:"provider"`
	Slot        string `json:"slot"`
}
type Migration struct {
	FromSchema uint16 `json:"from_schema"`
	ToSchema   uint16 `json:"to_schema"`
	ID         string `json:"id"`
}
type RollbackBoundary struct {
	ID            string                  `json:"id"`
	Kind          string                  `json:"kind"`
	Target        string                  `json:"target"`
	Resources     []Projection            `json:"resources"`
	Finalizations []FinalizationReference `json:"finalizations"`
}

type legacyV1 struct {
	SchemaVersion   uint16                     `json:"schema_version"`
	CompilerVersion string                     `json:"compiler_version"`
	Pack            PackIdentity               `json:"pack"`
	InputID         string                     `json:"input_id"`
	ResolvedIRID    string                     `json:"resolved_ir_id"`
	Profile         ProfileIdentity            `json:"profile"`
	Target          string                     `json:"target"`
	Projections     []Projection               `json:"projections"`
	RoleModels      []RoleModelProvenance      `json:"role_models"`
	Capabilities    []CapabilityProvenance     `json:"capabilities"`
	Reconciliation  ReconciliationProvenance   `json:"reconciliation"`
	Finalizations   []FinalizationReference    `json:"finalizations"`
	Extensions      map[string]json.RawMessage `json:"extensions,omitempty"`
}
type legacyV2 legacyV1

// Manifest records only non-secret facts needed to bind an admitted projection
// to its input, IR, ownership/reconciliation, and safe finalization seam.
type Manifest struct {
	SchemaVersion    uint16                     `json:"schema_version"`
	CompilerVersion  string                     `json:"compiler_version"`
	Pack             PackIdentity               `json:"pack"`
	InputID          string                     `json:"input_id"`
	ResolvedIRID     string                     `json:"resolved_ir_id"`
	Profile          ProfileIdentity            `json:"profile"`
	Target           string                     `json:"target"`
	Projections      []Projection               `json:"projections"`
	RollbackBoundary RollbackBoundary           `json:"rollback_boundary"`
	RoleModels       []RoleModelProvenance      `json:"role_models"`
	Capabilities     []CapabilityProvenance     `json:"capabilities"`
	Reconciliation   ReconciliationProvenance   `json:"reconciliation"`
	Finalizations    []FinalizationReference    `json:"finalizations"`
	Migrations       []Migration                `json:"migrations,omitempty"`
	Extensions       map[string]json.RawMessage `json:"extensions,omitempty"`
}

func (m Manifest) ProjectionsCopy() []Projection              { return slices.Clone(m.Projections) }
func (m Manifest) RoleModelsCopy() []RoleModelProvenance      { return slices.Clone(m.RoleModels) }
func (m Manifest) CapabilitiesCopy() []CapabilityProvenance   { return slices.Clone(m.Capabilities) }
func (m Manifest) FinalizationsCopy() []FinalizationReference { return slices.Clone(m.Finalizations) }
func (m Manifest) MigrationsCopy() []Migration                { return slices.Clone(m.Migrations) }
func (m Manifest) RollbackBoundaryCopy() RollbackBoundary {
	out := m.RollbackBoundary
	out.Resources, out.Finalizations = slices.Clone(out.Resources), slices.Clone(out.Finalizations)
	return out
}
func (m Manifest) ExtensionsCopy() map[string]json.RawMessage {
	if m.Extensions == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(m.Extensions))
	for key, value := range m.Extensions {
		out[key] = append(json.RawMessage(nil), value...)
	}
	return out
}

// Encode validates and emits one compact UTF-8 JSON representation. Its field
// order follows Manifest's declaration order; projections sort by semantic key.
func Encode(manifest Manifest) ([]byte, error) {
	normalized, err := normalize(manifest)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return nil, codecError(CodeInvalidField, "$", "manifest cannot be encoded")
	}
	return data, nil
}

// Decode accepts only a single, strict v3 JSON object and returns a zero value
// on every contract error.
// Migrate accepts only registered fixed v1/v2 schemas and returns canonical v3.
// It never infers missing provenance or accepts an unregistered schema.
func Migrate(data []byte) (Manifest, error) {
	if err := rejectDuplicateFields(data); err != nil {
		return Manifest{}, err
	}
	var header struct {
		SchemaVersion uint16 `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return Manifest{}, decodeError(err)
	}
	var legacy legacyV1
	switch header.SchemaVersion {
	case 1:
		if err := decodeStrict(data, &legacy); err != nil {
			return Manifest{}, err
		}
	case 2:
		var v2 legacyV2
		if err := decodeStrict(data, &v2); err != nil {
			return Manifest{}, err
		}
		legacy = legacyV1(v2)
	default:
		return Manifest{}, codecError(CodeUnsupportedSchema, "schema_version", "unsupported legacy schema_version; registered versions are 1 and 2")
	}
	resources := slices.Clone(legacy.Projections)
	finalizations := slices.Clone(legacy.Finalizations)
	sort.Slice(resources, func(i, j int) bool { return resources[i].Path < resources[j].Path })
	sort.Slice(finalizations, func(i, j int) bool { return finalizations[i].Path < finalizations[j].Path })
	candidate := Manifest{SchemaVersion: SchemaV3, CompilerVersion: legacy.CompilerVersion, Pack: legacy.Pack, InputID: legacy.InputID, ResolvedIRID: legacy.ResolvedIRID, Profile: legacy.Profile, Target: legacy.Target, Projections: legacy.Projections, RollbackBoundary: RollbackBoundary{ID: "validation-only", Kind: "selected-target-backup-v1", Target: legacy.Target, Resources: resources, Finalizations: finalizations}, RoleModels: legacy.RoleModels, Capabilities: legacy.Capabilities, Reconciliation: legacy.Reconciliation, Finalizations: legacy.Finalizations, Extensions: legacy.Extensions}
	if _, err := normalize(candidate); err != nil {
		return Manifest{}, err
	}
	return Manifest{}, codecError(CodeInvalidField, "rollback_boundary", "rollback boundary provenance cannot be reconstructed")
}

func decodeStrict(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return decodeError(err)
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return codecError(CodeMalformedJSON, "$", "manifest must contain one JSON value")
	}
	return nil
}

type Outcome string

const (
	OutcomeNoop      Outcome = "noop"
	OutcomeUpdate    Outcome = "update"
	OutcomeMigration Outcome = "migration"
	OutcomeConflict  Outcome = "conflict"
)

type Explanation struct{ Code, Remediation string }
type Decision struct {
	Outcome                          Outcome
	CurrentIdentity, DesiredIdentity string
	CurrentHash, DesiredHash         string
	explanation                      Explanation
}

func (d Decision) Explanation() Explanation { return d.explanation }

// Reconcile compares strict current and desired manifests without I/O. Invalid,
// unsupported, or lossy inputs return typed errors and a zero decision.
func Reconcile(current, desired []byte) (Decision, error) {
	old, migrated, err := compatible(current)
	if err != nil {
		return Decision{}, err
	}
	next, _, err := compatible(desired)
	if err != nil {
		return Decision{}, err
	}
	oldBytes, _ := Encode(old)
	nextBytes, _ := Encode(next)
	d := Decision{CurrentIdentity: manifestIdentity(oldBytes), DesiredIdentity: manifestIdentity(nextBytes), CurrentHash: fmt.Sprintf("%x", sha256.Sum256(oldBytes)), DesiredHash: fmt.Sprintf("%x", sha256.Sum256(nextBytes))}
	if hasConflict(old) || hasConflict(next) {
		d.Outcome, d.explanation = OutcomeConflict, Explanation{"ambiguous_ownership", "resolve conflicting ownership before apply"}
	} else if migrated {
		d.Outcome, d.explanation = OutcomeMigration, Explanation{"registered_migration", "apply the registered v3 migration"}
	} else if d.CurrentHash == d.DesiredHash {
		d.Outcome, d.explanation = OutcomeNoop, Explanation{"unchanged", "no action required"}
	} else {
		d.Outcome, d.explanation = OutcomeUpdate, Explanation{"desired_changed", "apply the deterministic manifest update"}
	}
	return d, nil
}
func manifestIdentity(canonical []byte) string {
	sum := sha256.Sum256(append([]byte("lore.manifest.identity.v3\x00"), canonical...))
	return fmt.Sprintf("manifest-v3:%x", sum)
}
func compatible(data []byte) (Manifest, bool, error) {
	var h struct {
		SchemaVersion uint16 `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &h); err != nil {
		return Manifest{}, false, decodeError(err)
	}
	if h.SchemaVersion == SchemaV3 {
		m, err := Decode(data)
		return m, false, err
	}
	m, err := Migrate(data)
	return m, true, err
}
func hasConflict(m Manifest) bool {
	for _, p := range m.Projections {
		if p.Ownership == "conflict" {
			return true
		}
	}
	return false
}

func Decode(data []byte) (Manifest, error) {
	if err := rejectDuplicateFields(data); err != nil {
		return Manifest{}, err
	}
	if err := validateBoundaryJSON(data); err != nil {
		return Manifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, decodeError(err)
	}
	if token, err := decoder.Token(); err != io.EOF || token != nil {
		return Manifest{}, codecError(CodeMalformedJSON, "$", "manifest must contain one JSON value")
	}
	normalized, err := normalize(manifest)
	if err != nil {
		return Manifest{}, err
	}
	return normalized, nil
}

func decodeError(err error) error {
	message := err.Error()
	const unknownPrefix = "json: unknown field "
	if strings.HasPrefix(message, unknownPrefix) {
		field := strings.Trim(strings.TrimPrefix(message, unknownPrefix), `"`)
		return codecError(CodeUnknownField, field, "unknown normative field: "+field)
	}
	return codecError(CodeMalformedJSON, "$", "malformed manifest JSON")
}

func normalize(manifest Manifest) (Manifest, error) {
	manifest.Projections = slices.Clone(manifest.Projections)
	manifest.RoleModels = slices.Clone(manifest.RoleModels)
	manifest.Capabilities = slices.Clone(manifest.Capabilities)
	manifest.Finalizations = slices.Clone(manifest.Finalizations)
	manifest.Migrations = slices.Clone(manifest.Migrations)
	manifest.RollbackBoundary = manifest.RollbackBoundaryCopy()
	manifest.Extensions = manifest.ExtensionsCopy()
	if manifest.SchemaVersion != SchemaV3 {
		return Manifest{}, codecError(CodeUnsupportedSchema, "schema_version", "unsupported schema_version; supported version is 3")
	}
	if err := require("compiler_version", manifest.CompilerVersion); err != nil {
		return Manifest{}, err
	}
	if err := require("pack.id", manifest.Pack.ID); err != nil {
		return Manifest{}, err
	}
	if err := require("pack.version", manifest.Pack.Version); err != nil {
		return Manifest{}, err
	}
	if err := require("input_id", manifest.InputID); err != nil {
		return Manifest{}, err
	}
	if err := require("resolved_ir_id", manifest.ResolvedIRID); err != nil {
		return Manifest{}, err
	}
	if err := require("profile.id", manifest.Profile.ID); err != nil {
		return Manifest{}, err
	}
	if err := require("profile.version", manifest.Profile.Version); err != nil {
		return Manifest{}, err
	}
	if manifest.Profile.Scope != "global" && manifest.Profile.Scope != "project" {
		return Manifest{}, codecError(CodeInvalidField, "profile.scope", "profile.scope must be global or project")
	}
	if err := require("target", manifest.Target); err != nil {
		return Manifest{}, err
	}
	if manifest.Projections == nil {
		return Manifest{}, codecError(CodeRequiredField, "projections", "projections is required")
	}
	result := manifest
	result.Projections = append([]Projection(nil), manifest.Projections...)
	sort.Slice(result.Projections, func(i, j int) bool { return result.Projections[i].Path < result.Projections[j].Path })
	seen := make(map[string]struct{}, len(result.Projections))
	for i, projection := range result.Projections {
		if err := require(fmt.Sprintf("projections[%d].path", i), projection.Path); err != nil {
			return Manifest{}, err
		}
		if _, ok := seen[projection.Path]; ok {
			return Manifest{}, codecError(CodeDuplicateRecord, fmt.Sprintf("projections[%d].path", i), "duplicate projection path")
		}
		seen[projection.Path] = struct{}{}
		if projection.Ownership != "replace" && projection.Ownership != "marker-merge" && projection.Ownership != "additive" && projection.Ownership != "conflict" {
			return Manifest{}, codecError(CodeInvalidField, fmt.Sprintf("projections[%d].ownership", i), "unsupported projection ownership")
		}
		if err := require(fmt.Sprintf("projections[%d].hash", i), projection.Hash); err != nil {
			return Manifest{}, err
		}
	}
	if err := normalizeProvenance(&result); err != nil {
		return Manifest{}, err
	}
	if err := validateRollback(result); err != nil {
		return Manifest{}, err
	}
	sort.Slice(result.Migrations, func(i, j int) bool { return result.Migrations[i].ID < result.Migrations[j].ID })
	for i, migration := range result.Migrations {
		if migration.ToSchema != SchemaV3 || migration.FromSchema >= SchemaV3 || migration.ID != fmt.Sprintf("manifest-v%d-to-v3", migration.FromSchema) {
			return Manifest{}, codecError(CodeInvalidField, fmt.Sprintf("migrations[%d]", i), "invalid migration provenance")
		}
		if i > 0 && migration.ID == result.Migrations[i-1].ID {
			return Manifest{}, codecError(CodeDuplicateRecord, "migrations", "duplicate migration provenance")
		}
	}
	return result, nil
}

func normalizeProvenance(m *Manifest) error {
	if m.RoleModels == nil || m.Capabilities == nil || m.Finalizations == nil {
		return codecError(CodeRequiredField, "provenance", "complete provenance is required")
	}
	sort.Slice(m.RoleModels, func(i, j int) bool { return m.RoleModels[i].Role < m.RoleModels[j].Role })
	for i, v := range m.RoleModels {
		if err := require(fmt.Sprintf("role_models[%d].role", i), v.Role); err != nil {
			return err
		}
		if err := require(fmt.Sprintf("role_models[%d].model", i), v.Model); err != nil {
			return err
		}
		if err := require(fmt.Sprintf("role_models[%d].source", i), v.Source); err != nil {
			return err
		}
		if err := require(fmt.Sprintf("role_models[%d].location", i), v.Location); err != nil {
			return err
		}
		if i > 0 && v.Role == m.RoleModels[i-1].Role {
			return codecError(CodeDuplicateRecord, "role_models", "duplicate role provenance")
		}
	}
	sort.Slice(m.Capabilities, func(i, j int) bool { return m.Capabilities[i].ID < m.Capabilities[j].ID })
	for i, v := range m.Capabilities {
		if err := require(fmt.Sprintf("capabilities[%d].id", i), v.ID); err != nil {
			return err
		}
		if err := require(fmt.Sprintf("capabilities[%d].state", i), v.State); err != nil {
			return err
		}
		if err := require(fmt.Sprintf("capabilities[%d].reason", i), v.Reason); err != nil {
			return err
		}
		if i > 0 && v.ID == m.Capabilities[i-1].ID {
			return codecError(CodeDuplicateRecord, "capabilities", "duplicate capability provenance")
		}
	}
	if err := require("reconciliation.owner", m.Reconciliation.Owner); err != nil {
		return err
	}
	if err := require("reconciliation.decision", m.Reconciliation.Decision); err != nil {
		return err
	}
	sort.Slice(m.Finalizations, func(i, j int) bool { return m.Finalizations[i].Path < m.Finalizations[j].Path })
	for i, v := range m.Finalizations {
		if err := require(fmt.Sprintf("finalizations[%d].path", i), v.Path); err != nil {
			return err
		}
		if err := require(fmt.Sprintf("finalizations[%d].finalizer_id", i), v.FinalizerID); err != nil {
			return err
		}
		if err := require(fmt.Sprintf("finalizations[%d].provider", i), v.Provider); err != nil {
			return err
		}
		if err := require(fmt.Sprintf("finalizations[%d].slot", i), v.Slot); err != nil {
			return err
		}
		if i > 0 && v.Path == m.Finalizations[i-1].Path {
			return codecError(CodeDuplicateRecord, fmt.Sprintf("finalizations[%d].path", i), "duplicate finalization path")
		}
	}
	keys := make([]string, 0, len(m.Extensions))
	for key := range m.Extensions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		path := fmt.Sprintf("extensions[%q]", key)
		if !safeExtensionKey(key) || prohibitedKey(key) {
			return codecError(CodeInvalidField, path, "extension contains secret-shaped data")
		}
		var value any
		if json.Unmarshal(m.Extensions[key], &value) != nil {
			return codecError(CodeInvalidField, path, "invalid extension JSON")
		}
		if err := secretValue(value, path); err != nil {
			return err
		}
		canonical, _ := json.Marshal(value)
		m.Extensions[key] = append(json.RawMessage(nil), canonical...)
	}
	return nil
}

func safeExtensionKey(key string) bool {
	parts := strings.Split(key, "/")
	if len(parts) != 2 || parts[0] == "lore.dev" || !strings.Contains(parts[0], ".") {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.ToLower(part) != part || strings.ContainsAny(part, " \t\r\n") {
			return false
		}
	}
	return true
}

var authScalar = regexp.MustCompile(`(?i)^(bearer|basic)[ \t]+[^\r\n]+$`)

func prohibitedKey(key string) bool {
	key = strings.ToLower(key)
	for _, term := range []string{"secret", "token", "password", "bearer", "credential", "api_key", "apikey", "authorization"} {
		if strings.Contains(key, term) {
			return true
		}
	}
	return false
}
func secretValue(value any, path string) error {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			next := path + "." + key
			if prohibitedKey(key) {
				return codecError(CodeInvalidField, next, "extension contains secret-shaped data")
			}
			if err := secretValue(v[key], next); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range v {
			if err := secretValue(child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case string:
		if authScalar.MatchString(v) {
			return codecError(CodeInvalidField, path, "extension contains secret-shaped data")
		}
	}
	return nil
}

var rollbackID = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,127}$`)

func validateRollback(m Manifest) error {
	b := m.RollbackBoundary
	if b.ID == "" && b.Kind == "" && b.Target == "" && b.Resources == nil && b.Finalizations == nil {
		return codecError(CodeRequiredField, "rollback_boundary", "rollback_boundary is required")
	}
	if err := require("rollback_boundary.id", b.ID); err != nil {
		return err
	}
	if !rollbackID.MatchString(b.ID) {
		return codecError(CodeInvalidField, "rollback_boundary.id", "invalid rollback boundary")
	}
	if err := require("rollback_boundary.kind", b.Kind); err != nil {
		return err
	}
	if b.Kind != "selected-target-backup-v1" {
		return codecError(CodeInvalidField, "rollback_boundary.kind", "unsupported rollback boundary kind")
	}
	if err := require("rollback_boundary.target", b.Target); err != nil {
		return err
	}
	if b.Target != m.Target {
		return codecError(CodeInvalidField, "rollback_boundary.target", "rollback boundary target mismatch")
	}
	if b.Resources == nil {
		return codecError(CodeRequiredField, "rollback_boundary.resources", "rollback_boundary.resources is required")
	}
	if b.Finalizations == nil {
		return codecError(CodeRequiredField, "rollback_boundary.finalizations", "rollback_boundary.finalizations is required")
	}
	for i, v := range b.Resources {
		path := fmt.Sprintf("rollback_boundary.resources[%d]", i)
		if err := require(path+".path", v.Path); err != nil {
			return err
		}
		if i > 0 && v.Path <= b.Resources[i-1].Path {
			return codecError(CodeInvalidField, path+".path", "rollback resources must be unique and sorted")
		}
	}
	if len(b.Resources) != len(m.Projections) {
		return codecError(CodeInvalidField, "rollback_boundary.resources", "rollback resources mismatch")
	}
	for i, v := range b.Resources {
		want, path := m.Projections[i], fmt.Sprintf("rollback_boundary.resources[%d]", i)
		field := ".hash"
		if v.Path != want.Path {
			field = ".path"
		} else if v.Ownership != want.Ownership {
			field = ".ownership"
		} else if v.Hash == want.Hash {
			continue
		}
		return codecError(CodeInvalidField, path+field, "rollback resource mismatch")
	}
	resources := map[string]struct{}{}
	for _, v := range b.Resources {
		resources[v.Path] = struct{}{}
	}
	for i, v := range b.Finalizations {
		path := fmt.Sprintf("rollback_boundary.finalizations[%d]", i)
		if i > 0 && v.Path <= b.Finalizations[i-1].Path {
			return codecError(CodeInvalidField, path+".path", "rollback finalizations must be unique and sorted")
		}
		if _, ok := resources[v.Path]; !ok {
			return codecError(CodeInvalidField, path+".path", "rollback finalization is not projected")
		}
	}
	if len(b.Finalizations) != len(m.Finalizations) {
		return codecError(CodeInvalidField, "rollback_boundary.finalizations", "rollback finalizations mismatch")
	}
	for i, v := range b.Finalizations {
		want, path := m.Finalizations[i], fmt.Sprintf("rollback_boundary.finalizations[%d]", i)
		field := ".slot"
		if v.Path != want.Path {
			field = ".path"
		} else if v.FinalizerID != want.FinalizerID {
			field = ".finalizer_id"
		} else if v.Provider != want.Provider {
			field = ".provider"
		} else if v.Slot == want.Slot {
			continue
		}
		return codecError(CodeInvalidField, path+field, "rollback finalization mismatch")
	}
	return nil
}

func validateBoundaryJSON(data []byte) error {
	var root map[string]json.RawMessage
	if json.Unmarshal(data, &root) != nil {
		return nil
	}
	raw, ok := root["rollback_boundary"]
	if !ok {
		return codecError(CodeRequiredField, "rollback_boundary", "rollback_boundary is required")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) || trimmed[0] != '{' {
		return codecError(CodeInvalidField, "rollback_boundary", "rollback_boundary must be an object")
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(raw, &members) != nil {
		return codecError(CodeInvalidField, "rollback_boundary", "rollback_boundary must be an object")
	}
	for _, name := range []string{"id", "kind", "target"} {
		if err := requiredJSONString(members, name, "rollback_boundary."+name); err != nil {
			return err
		}
	}
	if err := requiredObjectArray(members, "resources", "rollback_boundary.resources", []string{"path", "ownership", "hash"}); err != nil {
		return err
	}
	return requiredObjectArray(members, "finalizations", "rollback_boundary.finalizations", []string{"path", "finalizer_id", "provider", "slot"})
}
func requiredJSONString(object map[string]json.RawMessage, name, path string) error {
	raw, ok := object[name]
	if !ok {
		return codecError(CodeRequiredField, path, path+" is required")
	}
	var value string
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &value) != nil {
		return codecError(CodeInvalidField, path, "invalid rollback boundary member type")
	}
	return nil
}
func requiredObjectArray(object map[string]json.RawMessage, name, path string, fields []string) error {
	raw, ok := object[name]
	if !ok {
		return codecError(CodeRequiredField, path, path+" is required")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return codecError(CodeInvalidField, path, "rollback boundary list must be an array")
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return codecError(CodeInvalidField, path, "rollback boundary list must be an array")
	}
	for i, value := range values {
		var record map[string]json.RawMessage
		item := fmt.Sprintf("%s[%d]", path, i)
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &record) != nil {
			return codecError(CodeInvalidField, item, "rollback boundary record must be an object")
		}
		for _, field := range fields {
			if err := requiredJSONString(record, field, item+"."+field); err != nil {
				return err
			}
		}
	}
	return nil
}

func require(path, value string) error {
	if strings.TrimSpace(value) == "" {
		return codecError(CodeRequiredField, path, path+" is required")
	}
	return nil
}

func rejectDuplicateFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanValue(decoder, "$", true); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return codecError(CodeMalformedJSON, "$", "manifest must contain one JSON value")
	}
	return nil
}

func scanValue(decoder *json.Decoder, path string, root bool) error {
	token, err := decoder.Token()
	if err != nil {
		return codecError(CodeMalformedJSON, "$", "malformed manifest JSON")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return codecError(CodeMalformedJSON, "$", "malformed manifest JSON")
			}
			key, ok := keyToken.(string)
			if !ok {
				return codecError(CodeMalformedJSON, "$", "malformed manifest JSON")
			}
			fieldPath := key
			if !root {
				fieldPath = path + "." + key
			}
			if _, exists := seen[key]; exists {
				return codecError(CodeDuplicateField, fieldPath, "duplicate JSON field: "+fieldPath)
			}
			seen[key] = struct{}{}
			if err := scanValue(decoder, fieldPath, false); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return codecError(CodeMalformedJSON, "$", "malformed manifest JSON")
		}
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := scanValue(decoder, fmt.Sprintf("%s[%d]", path, index), false); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return codecError(CodeMalformedJSON, "$", "malformed manifest JSON")
		}
	default:
		return codecError(CodeMalformedJSON, "$", "malformed manifest JSON")
	}
	return nil
}
