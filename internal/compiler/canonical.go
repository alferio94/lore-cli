package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

const (
	inputDomain = "lore.compiler.input.v1\x00"
	irDomain    = "lore.compiler.ir.v1\x00"
)

func identity(domain string, value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(append([]byte(domain), data...))
	return hex.EncodeToString(digest[:]), nil
}

func cloneInput(input Input) Input {
	result := input
	result.Profiles = append([]Profile(nil), input.Profiles...)
	for i := range result.Profiles {
		result.Profiles[i].Roles = cloneStrings(input.Profiles[i].Roles)
	}
	result.Candidates = append([]Candidate(nil), input.Candidates...)
	result.RoleOverrides = append([]RoleOverride(nil), input.RoleOverrides...)
	result.Requested = append([]CapabilityRequest(nil), input.Requested...)
	result.Extensions = cloneStrings(input.Extensions)
	result.CredentialSlots = append([]CredentialRef(nil), input.CredentialSlots...)
	return result
}

func cloneStrings(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func validCompilerVersion(version string) bool {
	const prefix = "canonical-capability-profile-compiler/v"
	if !strings.HasPrefix(version, prefix) {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(version, prefix), ".")
	if len(parts) != 3 {
		return false
	}
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return false
		}
		value, err := strconv.ParseUint(part, 10, 32)
		if err != nil || (index == 0 && value != 1) {
			return false
		}
	}
	return true
}

func sortedStrings(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
