package install

type Phase string

const (
	PhasePrepare  Phase = "prepare"
	PhaseSeal     Phase = "seal"
	PhaseBackup   Phase = "backup"
	PhaseWrite    Phase = "write"
	PhaseFinalize Phase = "finalize"
	PhaseProfile  Phase = "profile"
	PhasePublish  Phase = "publish"
	PhaseRollback Phase = "rollback"
)

type EventKind string

const (
	EventStarted   EventKind = "started"
	EventProgress  EventKind = "progress"
	EventCompleted EventKind = "completed"
	EventFailed    EventKind = "failed"
)

type Progress struct {
	Completed int
	Total     int
}
type Event struct {
	Seq       uint64
	Phase     Phase
	Kind      EventKind
	Progress  Progress
	Operation *Operation
	Error     *InstallError
}

func (e Event) Clone() Event {
	if e.Operation != nil {
		operation := *e.Operation
		e.Operation = &operation
	}
	return e
}
