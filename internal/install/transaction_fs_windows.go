//go:build windows

package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"golang.org/x/sys/windows"
)

type windowsTransactionFS struct {
	root, journal, prepare, cleanupRoot string
	identity                            [sha256.Size]byte
	createdDirs                         []string
}

func newTransactionFSPlatform(root string, identity [sha256.Size]byte) (transactionFSPlatform, error) {
	if !filepath.IsAbs(root) || !windowsTransactionPathSafe(root, true) || identity == [sha256.Size]byte{} {
		return nil, errTransactionFSUnsafePath
	}
	base := filepath.Join(filepath.Dir(root), ".lore-transaction-v1-"+hex.EncodeToString(identity[:]))
	return &windowsTransactionFS{
		root:        filepath.Clean(root),
		journal:     base,
		prepare:     base + ".prepare",
		cleanupRoot: base + ".cleanup",
		identity:    identity,
	}, nil
}

func (p *windowsTransactionFS) journalRoot() string { return p.journal }

func (p *windowsTransactionFS) recover() error {
	if err := p.removeResidue(p.cleanupRoot); err != nil {
		return err
	}
	if err := p.removeResidue(p.prepare); err != nil {
		return err
	}
	if _, err := os.Lstat(p.journal); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil || !windowsTransactionPathSafe(p.journal, true) {
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
	if err != nil || !validWindowsDurableJournal(p.journal, record) {
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

func (p *windowsTransactionFS) begin() error {
	for _, path := range []string{p.journal, p.prepare, p.cleanupRoot} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return errTransactionFSIO
		}
	}
	if err := os.Mkdir(p.prepare, 0o700); err != nil || windowsProtectFile(p.prepare) != nil {
		return errTransactionFSIO
	}
	backups := filepath.Join(p.prepare, "backups")
	if err := os.Mkdir(backups, 0o700); err != nil || windowsProtectFile(backups) != nil {
		return errTransactionFSIO
	}
	return nil
}

func (p *windowsTransactionFS) backup(rel string, index int) (transactionFSState, error) {
	path, pathErr := transactionNativePath(p.root, rel)
	if pathErr != nil {
		return transactionFSState{}, pathErr
	}
	if !windowsTransactionAncestorsSafe(p.root, path) {
		return transactionFSState{}, errTransactionFSUnsafePath
	}
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return transactionFSState{path: rel}, nil
	} else if err != nil || !windowsTransactionPathSafe(path, false) {
		return transactionFSState{}, errTransactionFSUnsafePath
	}
	acl, err := windowsTransactionACL(path)
	if err != nil {
		return transactionFSState{}, errTransactionFSIO
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return transactionFSState{}, errTransactionFSIO
	}
	backup := fmt.Sprintf("backups/%06d", index)
	backupPath, pathErr := transactionNativePath(p.prepare, backup)
	if pathErr != nil {
		return transactionFSState{}, pathErr
	}
	file, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		err = windowsProtectFile(backupPath)
	}
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
	if err != nil {
		return transactionFSState{}, errTransactionFSIO
	}
	return transactionFSState{path: rel, backup: backup, exists: true, windowsACL: acl}, nil
}

func (p *windowsTransactionFS) seal(states []transactionFSState) error {
	created, err := p.plannedCreatedDirs(states)
	if err != nil {
		return err
	}
	for _, state := range states {
		path, pathErr := transactionNativePath(p.root, state.path)
		if pathErr != nil {
			return pathErr
		}
		temp, tempErr := p.transactionTempPath(path)
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
	if err := writeTransactionWindowsDurableFile(filepath.Join(p.prepare, "journal.json"), data); err != nil {
		return err
	}
	if err := p.writeMarkerAt(p.prepare, "prepared"); err != nil {
		return err
	}
	if err := moveWindowsTransactionDir(p.prepare, p.journal); err != nil {
		return err
	}
	p.createdDirs = created
	return nil
}

func (p *windowsTransactionFS) plannedCreatedDirs(states []transactionFSState) ([]string, error) {
	missing := make(map[string]struct{})
	for _, state := range states {
		path, err := transactionNativePath(p.root, state.path)
		if err != nil {
			return nil, err
		}
		for current := filepath.Dir(path); current != p.root; current = filepath.Dir(current) {
			if _, err := os.Lstat(current); errors.Is(err, os.ErrNotExist) {
				nativeRel, relErr := filepath.Rel(p.root, current)
				if relErr != nil {
					return nil, errTransactionFSUnsafePath
				}
				rel, relErr := transactionLogicalPathFromNative(nativeRel)
				if relErr != nil {
					return nil, errTransactionFSUnsafePath
				}
				missing[rel] = struct{}{}
				continue
			} else if err != nil || !windowsTransactionPathSafe(current, true) {
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

func (p *windowsTransactionFS) write(rel string, data []byte) error {
	path, pathErr := transactionNativePath(p.root, rel)
	if pathErr != nil {
		return pathErr
	}
	if err := p.ensureParents(filepath.Dir(path)); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil && !windowsTransactionPathSafe(path, false) {
		return errTransactionFSUnsafePath
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errTransactionFSUnsafePath
	}
	return p.atomicWrite(path, data)
}

func (p *windowsTransactionFS) removeTemps(states []transactionFSState) error {
	var result error
	for _, state := range states {
		target, err := transactionNativePath(p.root, state.path)
		if err != nil {
			result = errTransactionFSUnsafePath
			continue
		}
		path, err := p.transactionTempPath(target)
		if err != nil {
			result = errTransactionFSUnsafePath
			continue
		}
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil || !windowsTransactionPathSafe(path, false) {
			result = errTransactionFSIO
			continue
		}
		if err := os.Remove(path); err != nil {
			result = errTransactionFSIO
		}
	}
	return result
}

func (p *windowsTransactionFS) restore(state transactionFSState) error {
	path, pathErr := transactionNativePath(p.root, state.path)
	if pathErr != nil {
		return pathErr
	}
	if !windowsTransactionAncestorsSafe(p.root, path) {
		return errTransactionFSUnsafePath
	}
	if !state.exists {
		if _, err := os.Lstat(path); err == nil && !windowsTransactionPathSafe(path, false) {
			return errTransactionFSUnsafePath
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return errTransactionFSIO
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errTransactionFSIO
		}
		return nil
	}
	backup, pathErr := transactionNativePath(p.journal, state.backup)
	if pathErr != nil || !windowsTransactionPathSafe(backup, false) {
		return errTransactionFSIO
	}
	data, err := os.ReadFile(backup)
	if err != nil {
		return errTransactionFSIO
	}
	if _, err := os.Lstat(path); err == nil && !windowsTransactionPathSafe(path, false) {
		return errTransactionFSUnsafePath
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errTransactionFSIO
	}
	if err := p.atomicWrite(path, data); err != nil {
		return err
	}
	return restoreWindowsTransactionACL(path, state.windowsACL)
}

func (p *windowsTransactionFS) ensureParents(dir string) error {
	var missing []string
	for current := dir; current != p.root; current = filepath.Dir(current) {
		if _, err := os.Lstat(current); errors.Is(err, os.ErrNotExist) {
			nativeRel, relErr := filepath.Rel(p.root, current)
			if relErr != nil {
				return errTransactionFSUnsafePath
			}
			rel, relErr := transactionLogicalPathFromNative(nativeRel)
			if relErr != nil || !p.isPlannedCreatedDir(rel) {
				return errTransactionFSUnsafePath
			}
			missing = append(missing, current)
			continue
		} else if err != nil || !windowsTransactionPathSafe(current, true) {
			return errTransactionFSUnsafePath
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o700); err != nil || windowsProtectFile(missing[i]) != nil {
			return errTransactionFSIO
		}
	}
	return nil
}

func (p *windowsTransactionFS) isPlannedCreatedDir(rel string) bool {
	for _, planned := range p.createdDirs {
		if planned == rel {
			return true
		}
	}
	return false
}

func (p *windowsTransactionFS) atomicWrite(path string, data []byte) error {
	name, err := p.transactionTempPath(path)
	if err != nil {
		return err
	}
	tmp, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errTransactionFSIO
	}
	defer os.Remove(name)
	if err = windowsProtectFile(name); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = moveWindowsFile(name, path)
	}
	if err != nil {
		return errTransactionFSIO
	}
	return nil
}

func (p *windowsTransactionFS) transactionTempPath(path string) (string, error) {
	nativeRel, err := filepath.Rel(p.root, path)
	if err != nil {
		return "", errTransactionFSUnsafePath
	}
	rel, err := transactionLogicalPathFromNative(nativeRel)
	if err != nil {
		return "", errTransactionFSUnsafePath
	}
	hash := sha256.New()
	_, _ = hash.Write(p.identity[:])
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(rel))
	return filepath.Join(filepath.Dir(path), ".lore-write-v1-"+hex.EncodeToString(hash.Sum(nil))), nil
}

func windowsTransactionAncestorsSafe(root, path string) bool {
	for current := filepath.Dir(path); ; current = filepath.Dir(current) {
		if _, err := os.Lstat(current); err == nil {
			if !windowsTransactionPathSafe(current, true) {
				return false
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return false
		}
		if current == root {
			return true
		}
		if current == filepath.Dir(current) {
			return false
		}
	}
}

func windowsTransactionPathSafe(path string, directory bool) bool {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES|windows.READ_CONTROL, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(handle, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || !windowsHandleIsCurrentUserOnlyFile(handle) {
		return false
	}
	if directory {
		return info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	}
	return info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 && info.NumberOfLinks == 1
}

func windowsTransactionACL(path string) (string, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "", err
	}
	return sd.String(), nil
}

func restoreWindowsTransactionACL(path, encoded string) error {
	sd, err := windows.SecurityDescriptorFromString(encoded)
	if err != nil {
		return errTransactionFSIO
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return errTransactionFSIO
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		return errTransactionFSIO
	}
	return nil
}

func validWindowsDurableJournal(root string, record transactionFSDurableJournal) bool {
	for _, entry := range record.Entries {
		if entry.Exists {
			backup, err := transactionNativePath(root, entry.Backup)
			if err != nil || entry.Mode != 0 || entry.WindowsACL == "" || !windowsTransactionPathSafe(backup, false) {
				return false
			}
			if _, err := windows.SecurityDescriptorFromString(entry.WindowsACL); err != nil {
				return false
			}
		}
	}
	return true
}

func (p *windowsTransactionFS) removeCreatedDirs() error {
	var result error
	for i := len(p.createdDirs) - 1; i >= 0; i-- {
		path, pathErr := transactionNativePath(p.root, p.createdDirs[i])
		if pathErr != nil {
			result = errTransactionFSUnsafePath
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errTransactionFSIO
		}
	}
	return result
}

func (p *windowsTransactionFS) markCommitted() error { return p.writeMarkerAt(p.journal, "committed") }
func (p *windowsTransactionFS) markRestored() error  { return p.writeMarkerAt(p.journal, "restored") }

func (p *windowsTransactionFS) markerExists(name string) (bool, error) {
	path := filepath.Join(p.journal, name)
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil || !windowsTransactionPathSafe(path, false) {
		return false, errTransactionFSIO
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != name+"\n" {
		return false, errTransactionFSIO
	}
	return true, nil
}

func (p *windowsTransactionFS) writeMarkerAt(root, name string) error {
	path := filepath.Join(root, name)
	if windowsTransactionPathSafe(path, false) {
		data, err := os.ReadFile(path)
		if err != nil || string(data) != name+"\n" {
			return errTransactionFSIO
		}
		return nil
	}
	temp := path + ".prepare"
	if _, err := os.Lstat(temp); err == nil {
		if !windowsTransactionPathSafe(temp, false) || os.Remove(temp) != nil {
			return errTransactionFSIO
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errTransactionFSIO
	}
	if err := writeTransactionWindowsDurableFile(temp, []byte(name+"\n")); err != nil {
		return err
	}
	if err := moveWindowsFile(temp, path); err != nil {
		return errTransactionFSIO
	}
	return nil
}

func (p *windowsTransactionFS) removeMarkerTemps() error {
	for _, name := range []string{"committed.prepare", "restored.prepare"} {
		path := filepath.Join(p.journal, name)
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil || !windowsTransactionPathSafe(path, false) || os.Remove(path) != nil {
			return errTransactionFSIO
		}
	}
	return nil
}

func writeTransactionWindowsDurableFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		err = windowsProtectFile(path)
	}
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
	if err != nil {
		return errTransactionFSIO
	}
	return nil
}

func (p *windowsTransactionFS) readJournalHeader() ([]byte, error) {
	path := filepath.Join(p.journal, "journal.json")
	info, statErr := os.Lstat(path)
	if statErr != nil || info.Size() <= 0 || info.Size() > transactionFSJournalMaxBytes || !windowsTransactionPathSafe(path, false) {
		return nil, errTransactionFSIO
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errTransactionFSIO
	}
	return data, nil
}

func (p *windowsTransactionFS) removeResidue(path string) error {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil || !windowsTransactionPathSafe(path, true) {
		return errTransactionFSIO
	}
	if err := os.RemoveAll(path); err != nil {
		return errTransactionFSIO
	}
	return nil
}

func (p *windowsTransactionFS) cleanup() error {
	if _, err := os.Lstat(p.prepare); err == nil {
		if err := p.removeResidue(p.prepare); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errTransactionFSIO
	}
	if _, err := os.Lstat(p.journal); err == nil {
		if !windowsTransactionPathSafe(p.journal, true) {
			return errTransactionFSIO
		}
		if _, cleanupErr := os.Lstat(p.cleanupRoot); !errors.Is(cleanupErr, os.ErrNotExist) {
			return errTransactionFSIO
		}
		if err := moveWindowsTransactionDir(p.journal, p.cleanupRoot); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errTransactionFSIO
	}
	return p.removeResidue(p.cleanupRoot)
}

func moveWindowsTransactionDir(from, to string) error {
	source, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return errTransactionFSIO
	}
	destination, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return errTransactionFSIO
	}
	if err := windows.MoveFileEx(source, destination, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return errTransactionFSIO
	}
	return nil
}
