package install

const ResultSchemaVersion = "lore.install.result/v1"

type Status string

const (
	StatusReady        Status = "ready"
	StatusSucceeded    Status = "succeeded"
	StatusCancelled    Status = "cancelled"
	StatusRolledBack   Status = "rolled-back"
	StatusFailed       Status = "failed"
	StatusResidualRisk Status = "residual-risk"
)

type Operation struct {
	Resource string
	Action   string
}
type Guidance struct {
	Code    string
	Message string
}
type RollbackResult struct {
	Attempted bool
	Complete  bool
}
type Result struct {
	SchemaVersion string
	Mode          Mode
	Route         Route
	Target        TargetID
	Status        Status
	Admitted      bool
	ChangedState  bool
	ResidualRisk  bool
	Report        TransactionReport
	Operations    []Operation
	Guidance      []Guidance
	Warnings      []Guidance
	Rollback      RollbackResult
	Error         *InstallError
}

func (r Result) Clone() Result {
	r.Report = cloneTransactionReport(r.Report)
	r.Operations = append([]Operation(nil), r.Operations...)
	r.Guidance = append([]Guidance(nil), r.Guidance...)
	r.Warnings = append([]Guidance(nil), r.Warnings...)
	return r
}
