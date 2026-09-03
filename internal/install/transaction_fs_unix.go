//go:build darwin || linux

package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/sys/unix"
)

type unixTransactionFS struct {
	root, journal, prepare, cleanupRoot string
	identity                            [sha256.Size]byte
	createdDirs                         []string
}

func newTransactionFSPlatform(root string, identity [sha256.Size]byte) (transactionFSPlatform, error) {
	if !filepath.IsAbs(root) || !unixTransactionDirSafe(root) || identity == [sha256.Size]byte{} {
		return nil, errTransactionFSUnsafePath
	}
	base := filepath.Join(filepath.Dir(root), ".lore-transaction-v1-"+hex.EncodeToString(identity[:]))
	return &unixTransactionFS{
		root:        filepath.Clean(root),
		journal:     base,
		prepare:     base + ".prepare",
		cleanupRoot: base + ".cleanup",
		identity:    identity,
	}, nil
}

func (p *unixTransactionFS) journalRoot() string { return p.journal }

func (p *unixTransactionFS) recover() error {
	if err := p.removeResidue(p.cleanupRoot); err != nil {
		return err
	}
	if err := p.removeResidue(p.prepare); err != nil {
		return err
	}
	if _, err := os.Lstat(p.journal); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil || !unixTransactionJournalDirSafe(p.journal) {
		return errTransactionFSIO
	}
	if err := p.removeMarkerTemps(); err != nil {
		return err
	}
	committed, err := p.markerExists("committed")
	if err != nil {
		return err
	}
	restored, err := p.markerExists("restored")
	if err != nil || committed && restored {
		return errTransactionFSIO
	}
	if committed || restored {
		return p.cleanup()
	}
	prepared, err := p.markerExists("prepared")
	if err != nil || !prepared {
		return errTransactionFSIO
	}
	header, err := p.readJournalHeader()
	if err != nil {
		return err
	}
	record, err := decodeTransactionFSDurableJournal(header, p.identity)
	if err != nil || !validUnixDurableJournal(p.journal, record) {
		return errTransactionFSIO
	}
	p.createdDirs = append([]string(nil), record.CreatedDirs...)
	states := durableTransactionStates(record)
	failed := p.removeTemps(states) != nil
	for i := len(states) - 1; i >= 0; i-- {
		if p.restore(states[i]) != nil {
			failed = true
		}
	}
	if p.removeCreatedDirs() != nil {
		failed = true
	}
	if failed {
		return errTransactionFSIO
	}
	if err := p.markRestored(); err != nil {
		return err
	}
	return p.cleanup()
}

func (p *unixTransactionFS) begin() error {
	for _, path := range []string{p.journal, p.prepare, p.cleanupRoot} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errTransactionFSIO
		}
	}
	if err := os.Mkdir(p.prepare, 0o700); err != nil {
		return errTransactionFSIO
	}
	if err := os.Mkdir(filepath.Join(p.prepare, "backups"), 0o700); err != nil {
		return errTransactionFSIO
	}
	return syncTransactionUnixDir(filepath.Dir(p.root))
}

func (p *unixTransactionFS) backup(rel string, index int) (transactionFSState, error) {
	path := filepath.Join(p.root, rel)
	info, err := p.validate(path, true)
	if errors.Is(err, os.ErrNotExist) {
		return transactionFSState{path: rel}, nil
	}
	if err != nil || info.IsDir() {
		return transactionFSState{}, errTransactionFSUnsafePath
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return transactionFSState{}, errTransactionFSIO
	}
	file := os.NewFile(uintptr(fd), "transaction-source")
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || !safeTransactionUnixFile(stat) {
		return transactionFSState{}, errTransactionFSUnsafePath
	}
	backup := filepath.Join("backups", fmt.Sprintf("%06d", index))
	backupPath := filepath.Join(p.prepare, backup)
	out, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, err = out.ReadFrom(file)
	}
	if err == nil {
		err = out.Sync()
	}
	if out != nil {
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil || syncTransactionUnixDir(filepath.Dir(backupPath)) != nil {
		return transactionFSState{}, errTransactionFSIO
	}
	return transactionFSState{path: rel, backup: backup, exists: true, mode: uint32(info.Mode().Perm())}, nil
}

func (p *unixTransactionFS) seal(states []transactionFSState) error {
	created, err := p.plannedCreatedDirs(states)
	if err != nil {
		return err
	}
	for _, state := range states {
		temp, tempErr := p.transactionTempPath(filepath.Join(p.root, state.path))
		if tempErr != nil {
			return tempErr
		}
		if _, statErr := os.Lstat(temp); !errors.Is(statErr, os.ErrNotExist) {
			return errTransactionFSUnsafePath
		}
	}
	data, err := encodeTransactionFSDurableJournal(p.identity, states, created)
	if err != nil {
		return err
	}
	if err := writeTransactionUnixDurableFile(filepath.Join(p.prepare, "journal.json"), data); err != nil {
		return err
	}
	if err := p.writeMarkerAt(p.prepare, "prepared"); err != nil {
		return err
	}
	if err := os.Rename(p.prepare, p.journal); err != nil {
		return errTransactionFSIO
	}
	if err := syncTransactionUnixDir(filepath.Dir(p.root)); err != nil {
		return err
	}
	p.createdDirs = created
	return nil
}

func (p *unixTransactionFS) plannedCreatedDirs(states []transactionFSState) ([]string, error) {
	missing := make(map[string]struct{})
	for _, state := range states {
		for current := filepath.Dir(filepath.Join(p.root, state.path)); current != p.root; current = filepath.Dir(current) {
			info, err := os.Lstat(current)
			if errors.Is(err, os.ErrNotExist) {
				rel, relErr := filepath.Rel(p.root, current)
				if relErr != nil || !validTransactionRelativePath(rel) {
					return nil, errTransactionFSUnsafePath
				}
				missing[rel] = struct{}{}
				continue
			}
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
				return nil, errTransactionFSUnsafePath
			}
		}
	}
	created := make([]string, 0, len(missing))
	for rel := range missing {
		created = append(created, rel)
	}
	sort.Slice(created, func(i, j int) bool { return compareTransactionDirs(created[i], created[j]) < 0 })
	return created, nil
}

func (p *unixTransactionFS) write(rel string, data []byte) error {
	path := filepath.Join(p.root, rel)
	if err := p.ensureParents(filepath.Dir(path)); err != nil {
		return err
	}
	if _, err := p.validate(path, true); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errTransactionFSUnsafePath
	}
	return p.atomicWrite(path, data, 0o600)
}

func (p *unixTransactionFS) removeTemps(states []transactionFSState) error {
	var result error
	for _, state := range states {
		path, err := p.transactionTempPath(filepath.Join(p.root, state.path))
		if err != nil {
			result = errTransactionFSUnsafePath
			continue
		}
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil || !unixTransactionRegularFileSafe(path) {
			result = errTransactionFSIO
			continue
		}
		if err := os.Remove(path); err != nil || syncTransactionUnixDir(filepath.Dir(path)) != nil {
			result = errTransactionFSIO
		}
	}
	return result
}

func (p *unixTransactionFS) restore(state transactionFSState) error {
	path := filepath.Join(p.root, state.path)
	info, pathErr := p.validate(path, true)
	if !state.exists {
		if pathErr != nil && !errors.Is(pathErr, os.ErrNotExist) {
			return errTransactionFSUnsafePath
		}
		if pathErr == nil && info.IsDir() {
			return errTransactionFSUnsafePath
		}
		if err := os.Remove(path); errors.Is(err, os.ErrNotExist) {
			return nil
		} else if err != nil {
			return errTransactionFSIO
		}
		return syncTransactionUnixDir(filepath.Dir(path))
	}
	if pathErr != nil && !errors.Is(pathErr, os.ErrNotExist) {
		return errTransactionFSUnsafePath
	}
	backup := filepath.Join(p.journal, state.backup)
	if !unixTransactionRegularFileSafe(backup) {
		return errTransactionFSIO
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		return errTransactionFSIO
	}
	return p.atomicWrite(path, data, os.FileMode(state.mode))
}

func (p *unixTransactionFS) ensureParents(dir string) error {
	var missing []string
	for current := dir; current != p.root; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			rel, relErr := filepath.Rel(p.root, current)
			if relErr != nil || !p.isPlannedCreatedDir(rel) {
				return errTransactionFSUnsafePath
			}
			missing = append(missing, current)
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
			return errTransactionFSUnsafePath
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o700); err != nil {
			return errTransactionFSIO
		}
		if err := syncTransactionUnixDir(filepath.Dir(missing[i])); err != nil {
			return err
		}
	}
	return nil
}

func (p *unixTransactionFS) isPlannedCreatedDir(rel string) bool {
	for _, planned := range p.createdDirs {
		if planned == rel {
			return true
		}
	}
	return false
}

func (p *unixTransactionFS) atomicWrite(path string, data []byte, mode os.FileMode) error {
	name, err := p.transactionTempPath(path)
	if err != nil {
		return err
	}
	tmp, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errTransactionFSIO
	}
	defer os.Remove(name)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil || syncTransactionUnixDir(filepath.Dir(path)) != nil {
		return errTransactionFSIO
	}
	return nil
}

func (p *unixTransactionFS) transactionTempPath(path string) (string, error) {
	rel, err := filepath.Rel(p.root, path)
	if err != nil || !validTransactionRelativePath(rel) {
		return "", errTransactionFSUnsafePath
	}
	hash := sha256.New()
	_, _ = hash.Write(p.identity[:])
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(rel))
	return filepath.Join(filepath.Dir(path), ".lore-write-v1-"+hex.EncodeToString(hash.Sum(nil))), nil
}

func (p *unixTransactionFS) validate(path string, finalFile bool) (os.FileInfo, error) {
	rel, err := filepath.Rel(p.root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, errTransactionFSUnsafePath
	}
	current := p.root
	for _, part := range splitTransactionPath(rel) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return nil, statErr
		}
		isFinal := current == path
		if info.Mode()&os.ModeSymlink != 0 || (!isFinal && (!info.IsDir() || info.Mode().Perm() != 0o700)) {
			return nil, errTransactionFSUnsafePath
		}
		if isFinal {
			if finalFile {
				var stat unix.Stat_t
				if unix.Lstat(current, &stat) != nil || !safeTransactionUnixFile(stat) {
					return nil, errTransactionFSUnsafePath
				}
			}
			return info, nil
		}
	}
	return os.Lstat(path)
}

func splitTransactionPath(rel string) []string {
	var parts []string
	for rel != "." && rel != string(filepath.Separator) {
		parts = append([]string{filepath.Base(rel)}, parts...)
		rel = filepath.Dir(rel)
	}
	return parts
}

func unixTransactionDirSafe(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm() == 0o700
}

func unixTransactionJournalDirSafe(path string) bool { return unixTransactionDirSafe(path) }

func safeTransactionUnixFile(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&0o777 == 0o600 && stat.Nlink == 1
}

func unixTransactionRegularFileSafe(path string) bool {
	var stat unix.Stat_t
	return unix.Lstat(path, &stat) == nil && safeTransactionUnixFile(stat)
}

func validUnixDurableJournal(root string, record transactionFSDurableJournal) bool {
	for _, entry := range record.Entries {
		if entry.Exists {
			if entry.Mode != 0o600 || entry.WindowsACL != "" || !unixTransactionRegularFileSafe(filepath.Join(root, entry.Backup)) {
				return false
			}
		}
	}
	return true
}

func (p *unixTransactionFS) removeCreatedDirs() error {
	var result error
	for i := len(p.createdDirs) - 1; i >= 0; i-- {
		path := filepath.Join(p.root, p.createdDirs[i])
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errTransactionFSIO
		} else if err == nil && syncTransactionUnixDir(filepath.Dir(path)) != nil {
			result = errTransactionFSIO
		}
	}
	return result
}

func (p *unixTransactionFS) markCommitted() error { return p.writeMarkerAt(p.journal, "committed") }
func (p *unixTransactionFS) markRestored() error  { return p.writeMarkerAt(p.journal, "restored") }

func (p *unixTransactionFS) markerExists(name string) (bool, error) {
	path := filepath.Join(p.journal, name)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil || !unixTransactionRegularFileSafe(path) {
		return false, errTransactionFSIO
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != name+"\n" {
		return false, errTransactionFSIO
	}
	return true, nil
}

func (p *unixTransactionFS) writeMarkerAt(root, name string) error {
	path := filepath.Join(root, name)
	if unixTransactionRegularFileSafe(path) {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != name+"\n" {
			return errTransactionFSIO
		}
		return nil
	}
	temp := path + ".prepare"
	if _, err := os.Lstat(temp); err == nil {
		if !unixTransactionRegularFileSafe(temp) || os.Remove(temp) != nil {
			return errTransactionFSIO
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errTransactionFSIO
	}
	if err := writeTransactionUnixDurableFile(temp, []byte(name+"\n")); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil || syncTransactionUnixDir(root) != nil {
		return errTransactionFSIO
	}
	return nil
}

func (p *unixTransactionFS) removeMarkerTemps() error {
	for _, name := range []string{"committed.prepare", "restored.prepare"} {
		path := filepath.Join(p.journal, name)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil || !unixTransactionRegularFileSafe(path) || os.Remove(path) != nil {
			return errTransactionFSIO
		}
	}
	return syncTransactionUnixDir(p.journal)
}

func writeTransactionUnixDurableFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	if file != nil {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil || syncTransactionUnixDir(filepath.Dir(path)) != nil {
		return errTransactionFSIO
	}
	return nil
}

func (p *unixTransactionFS) readJournalHeader() ([]byte, error) {
	path := filepath.Join(p.journal, "journal.json")
	info, statErr := os.Lstat(path)
	if statErr != nil || info.Size() <= 0 || info.Size() > transactionFSJournalMaxBytes || !unixTransactionRegularFileSafe(path) {
		return nil, errTransactionFSIO
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errTransactionFSIO
	}
	return data, nil
}

func (p *unixTransactionFS) removeResidue(path string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil || !unixTransactionJournalDirSafe(path) {
		return errTransactionFSIO
	}
	if err := os.RemoveAll(path); err != nil {
		return errTransactionFSIO
	}
	return syncTransactionUnixDir(filepath.Dir(p.root))
}

func (p *unixTransactionFS) cleanup() error {
	if _, err := os.Lstat(p.prepare); err == nil {
		if err := p.removeResidue(p.prepare); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errTransactionFSIO
	}
	if _, err := os.Lstat(p.journal); err == nil {
		if !unixTransactionJournalDirSafe(p.journal) {
			return errTransactionFSIO
		}
		if _, cleanupErr := os.Lstat(p.cleanupRoot); !errors.Is(cleanupErr, os.ErrNotExist) {
			return errTransactionFSIO
		}
		if err := os.Rename(p.journal, p.cleanupRoot); err != nil {
			return errTransactionFSIO
		}
		if err := syncTransactionUnixDir(filepath.Dir(p.root)); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errTransactionFSIO
	}
	return p.removeResidue(p.cleanupRoot)
}

func syncTransactionUnixDir(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return errTransactionFSIO
	}
	defer file.Close()
	if err = file.Sync(); errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	if err != nil {
		return errTransactionFSIO
	}
	return nil
}
