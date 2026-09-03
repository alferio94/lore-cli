package compiler

import "fmt"

// Sensitive finalization is intentionally represented only by non-secret slots.
// Adapters may resolve them after a sealed plan reaches its protected final write.
type SensitiveProjection struct {
	FinalizerID, RedactionToken string
	Credential                  CredentialRef
}

func NewSensitiveProjection(ir ResolvedIR, finalizerID string, credential CredentialRef) (SensitiveProjection, error) {
	if !ir.Sealed() {
		return SensitiveProjection{}, fmt.Errorf("sensitive finalization requires a sealed plan")
	}
	if !ir.Admitted {
		return SensitiveProjection{}, fmt.Errorf("sensitive finalization requires an admitted plan")
	}
	if finalizerID == "" || credential.Provider == "" || credential.Slot == "" {
		return SensitiveProjection{}, fmt.Errorf("finalizer id and credential provider and slot are required")
	}
	for _, ref := range ir.CredentialSlots() {
		if ref == credential {
			return SensitiveProjection{FinalizerID: finalizerID, Credential: credential, RedactionToken: "<redacted:" + credential.Provider + "/" + credential.Slot + ">"}, nil
		}
	}
	return SensitiveProjection{}, fmt.Errorf("credential provider and slot are not declared by sealed plan")
}
