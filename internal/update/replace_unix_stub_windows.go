//go:build windows

package update

import (
	"context"
	"fmt"
)

func (s Service) applyUnix(_ context.Context, _ Plan) (Result, error) {
	return Result{}, fmt.Errorf("unix update flow is unavailable on windows builds")
}
