package opencodeready

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

// runCommand executes a command and captures output.
func runCommand(ctx context.Context, name string, args ...string) (Result, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return Result{
				Stdout:    string(out),
				Stderr:    "",
				ExitCode:  exitErr.ExitCode(),
				Error:     nil, // exit error is recoverable from exit code
			}, nil
		}
		return Result{
			Stdout:   string(out),
			Stderr:   "",
			ExitCode: -1,
			Error:    err,
		}, err
	}
	return Result{
		Stdout:   string(out),
		Stderr:   "",
		ExitCode: 0,
		Error:    nil,
	}, nil
}

// tempWritableProbe verifies that dir is writable by creating and removing a temp file.
// This is only used for explicitly probe-safe surfaces.
func tempWritableProbe(dir string) error {
	// Ensure dir exists
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &pathNotExistError{Path: dir}
		}
		return err
	}
	if !info.IsDir() {
		return &notDirError{Path: dir}
	}

	f, err := os.CreateTemp(dir, "probe-*")
	if err != nil {
		return err
	}
	_ = f.Close()
	_ = os.Remove(f.Name())
	return nil
}

type pathNotExistError struct {
	Path string
}

func (e *pathNotExistError) Error() string { return "path does not exist: " + e.Path }

type notDirError struct {
	Path string
}

func (e *notDirError) Error() string { return "path is not a directory: " + e.Path }