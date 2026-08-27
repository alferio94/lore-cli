package install

import (
	"context"
	"sync/atomic"
)

type (
	Mode  string
	Route string
)

const (
	RouteCanonical   Route = "canonical-sealed"
	RouteLegacy      Route = "explicit-legacy"
	ModeExplain      Mode  = "explain"
	ModeDryRun       Mode  = "dry-run"
	ModeApply        Mode  = "apply"
	ModeLegacyDryRun Mode  = "legacy-dry-run"
	ModeLegacyApply  Mode  = "legacy-apply"
)

type Request struct {
	Mode       Mode
	Target     TargetID
	Components []ComponentID
	AssumeYes  bool
}

func (r Request) Clone() Request {
	r.Components = append([]ComponentID(nil), r.Components...)
	return r
}

type preparedState struct{ consumed atomic.Bool }
type Prepared struct {
	request Request
	route   Route
	report  TransactionReport
	state   *preparedState
}

func newPrepared(request Request, route Route, report TransactionReport) Prepared {
	return Prepared{request: request.Clone(), route: route, report: cloneTransactionReport(report), state: &preparedState{}}
}
func (p Prepared) Request() Request          { return p.request.Clone() }
func (p Prepared) Route() Route              { return p.route }
func (p Prepared) Report() TransactionReport { return cloneTransactionReport(p.report) }
func (p Prepared) IsZero() bool              { return p.state == nil }
func (p Prepared) consume() bool {
	return p.state != nil && p.state.consumed.CompareAndSwap(false, true)
}

type Observer interface{ Observe(Event) }
type ObserverFunc func(Event)

func (f ObserverFunc) Observe(event Event) {
	if f != nil {
		f(event.Clone())
	}
}

type Workflow interface {
	Prepare(context.Context, Request) (Prepared, Result)
	Execute(context.Context, Prepared, Observer) Result
}
