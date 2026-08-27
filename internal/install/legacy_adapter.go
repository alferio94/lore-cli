package install

import "context"

// LegacyAdapter is the only compatibility seam for existing target Plan/Execute paths.
// RoutePolicy never invokes it; callers must first select an explicit legacy mode.
type LegacyAdapter interface {
	PrepareLegacy(context.Context, Request) (Prepared, Result)
	ExecuteLegacy(context.Context, Prepared, Observer) Result
}
