package install

type InstallErrorCode string

const (
	CodeCanonicalRouteDisabled InstallErrorCode = "canonical_route_disabled"
	CodeInvalidWorkflowRequest InstallErrorCode = "invalid_workflow_request"
	CodeInvalidRoutePolicy     InstallErrorCode = "invalid_route_policy"
	CodeInvalidRouteDemotion   InstallErrorCode = "invalid_route_demotion"
	CodeLegacyExecutionFailed  InstallErrorCode = "legacy_execution_failed"
)

func (c InstallErrorCode) Error() string { return string(c) }

type InstallError struct {
	code         InstallErrorCode
	path         string
	message      string
	retryable    bool
	residualRisk bool
}

func (e *InstallError) Error() string          { return e.message }
func (e *InstallError) Code() InstallErrorCode { return e.code }
func (e *InstallError) Path() string           { return e.path }
func (e *InstallError) Retryable() bool        { return e.retryable }
func (e *InstallError) ResidualRisk() bool     { return e.residualRisk }
func (e *InstallError) Is(target error) bool {
	code, ok := target.(InstallErrorCode)
	return ok && code == e.code
}
func newInstallError(code InstallErrorCode) *InstallError {
	switch code {
	case CodeCanonicalRouteDisabled:
		return &InstallError{code: code, path: "route_policy", message: "canonical route is disabled"}
	case CodeInvalidRouteDemotion:
		return &InstallError{code: code, path: "route_policy.gate", message: "route policy demotion is invalid"}
	case CodeInvalidRoutePolicy:
		return &InstallError{code: code, path: "route_policy", message: "route policy is invalid"}
	case CodeLegacyExecutionFailed:
		return &InstallError{code: code, path: "legacy.execution", message: "explicit legacy install failed"}
	default:
		return &InstallError{code: CodeInvalidWorkflowRequest, path: "request", message: "install workflow request is invalid"}
	}
}
