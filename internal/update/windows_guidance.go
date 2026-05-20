package update

import "fmt"

func applyWindows(plan Plan) Result {
	target := plan.Target.ResolvedPath
	if target == "" {
		target = plan.Target.ExecutablePath
	}
	return Result{
		Status:         ResultStatusUnsupported,
		ManualRecovery: fmt.Sprintf("Windows self-update is not supported in this slice. Download the matching release archive manually and replace %s after exiting Lore CLI. Pi runtime and ~/.pi remain untouched.", target),
	}
}
