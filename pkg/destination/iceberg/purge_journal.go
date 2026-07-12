package iceberg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	icebergcatalog "github.com/apache/iceberg-go/catalog"
	icebergio "github.com/apache/iceberg-go/io"
	icebergtable "github.com/apache/iceberg-go/table"
	"github.com/google/uuid"
	"gocloud.dev/gcerrors"
)

var errPurgeJournalRootUnavailable = errors.New("client-side purge requires a filesystem warehouse for durable cleanup journals")

const purgeJournalVersion = 1

type purgeJournal struct {
	Version             int        `json:"version"`
	Identifier          []string   `json:"identifier"`
	DeletionIdentifiers [][]string `json:"deletion_identifiers,omitempty"`
	TableUUID           string     `json:"table_uuid"`
	TableLocation       string     `json:"table_location"`
	Files               []string   `json:"files"`
	CreatedAt           string     `json:"created_at"`
}

func (d *Destination) createPurgeJournal(ctx context.Context, tbl *icebergtable.Table) (*purgeJournal, icebergio.IO, icebergio.IO, string, string, error) {
	lockToken, err := d.acquirePurgeLock(ctx, tbl.Identifier(), tbl.Metadata().TableUUID().String())
	if err != nil {
		return nil, nil, nil, "", "", err
	}
	tableFS, err := tbl.FS(ctx)
	if err != nil {
		_ = d.releasePurgeLockOwned(ctx, tbl.Identifier(), tbl.Metadata().TableUUID().String(), lockToken)
		return nil, nil, nil, "", "", fmt.Errorf("failed to load filesystem for purge journal: %w", err)
	}
	listable, ok := tableFS.(icebergio.ListableIO)
	if !ok {
		_ = d.releasePurgeLockOwned(ctx, tbl.Identifier(), tbl.Metadata().TableUUID().String(), lockToken)
		return nil, nil, nil, "", "", fmt.Errorf("table filesystem cannot list files for a durable purge journal")
	}
	files, err := filesUnderTableLocation(ctx, listable, tbl.Location())
	if err != nil {
		_ = d.releasePurgeLockOwned(ctx, tbl.Identifier(), tbl.Metadata().TableUUID().String(), lockToken)
		return nil, nil, nil, "", "", err
	}
	slices.Sort(files)

	journal := &purgeJournal{
		Version:             purgeJournalVersion,
		Identifier:          slices.Clone(tbl.Identifier()),
		DeletionIdentifiers: [][]string{deletionFenceIdentifier(tbl.Identifier())},
		TableUUID:           tbl.Metadata().TableUUID().String(),
		TableLocation:       tbl.Location(),
		Files:               files,
		CreatedAt:           time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := validatePurgeJournal(journal, tbl.Identifier()); err != nil {
		_ = d.releasePurgeLockOwned(ctx, tbl.Identifier(), tbl.Metadata().TableUUID().String(), lockToken)
		return nil, nil, nil, "", "", err
	}
	if err := d.validatePurgeJournalLocation(journal); err != nil {
		_ = d.releasePurgeLockOwned(ctx, tbl.Identifier(), tbl.Metadata().TableUUID().String(), lockToken)
		return nil, nil, nil, "", "", err
	}
	journalPath, err := d.purgeJournalPath(tbl.Identifier())
	if err != nil {
		_ = d.releasePurgeLockOwned(ctx, tbl.Identifier(), tbl.Metadata().TableUUID().String(), lockToken)
		return nil, nil, nil, "", "", err
	}
	journalFS, err := icebergio.LoadFS(ctx, d.cfg.Properties, journalPath)
	if err != nil {
		_ = d.releasePurgeLockOwned(ctx, tbl.Identifier(), tbl.Metadata().TableUUID().String(), lockToken)
		return nil, nil, nil, "", "", fmt.Errorf("failed to load purge journal filesystem: %w", err)
	}
	if err := writePurgeJournal(journalFS, journalPath, journal); err != nil {
		_ = d.releasePurgeLockOwned(ctx, tbl.Identifier(), tbl.Metadata().TableUUID().String(), lockToken)
		return nil, nil, nil, "", "", err
	}
	return journal, tableFS, journalFS, journalPath, lockToken, nil
}

func (d *Destination) resumePurgeJournal(ctx context.Context, ident icebergtable.Identifier) (retErr error) {
	journalPath, err := d.purgeJournalPath(ident)
	if err != nil {
		return err
	}
	journalFS, err := icebergio.LoadFS(ctx, d.cfg.Properties, journalPath)
	if err != nil {
		return fmt.Errorf("iceberg: failed to load purge journal filesystem: %w", err)
	}
	_, loadErr := d.catalog.LoadTable(ctx, ident)
	journal, readErr := readPurgeJournal(journalFS, journalPath, ident)
	if isObjectNotFound(readErr) {
		return nil
	}
	if loadErr == nil {
		if readErr != nil {
			return fmt.Errorf("iceberg: table %s exists; refusing corrupt journaled physical purge: %w", strings.Join(ident, "."), readErr)
		}
		claimToken, err := d.claimPurgeResume(ctx, ident, journal.TableUUID)
		if err != nil {
			return fmt.Errorf("iceberg: table %s still exists and its purge recovery cannot be claimed: %w", strings.Join(ident, "."), err)
		}
		if err := d.releasePurgeLockOwned(ctx, ident, journal.TableUUID, claimToken); err != nil {
			return fmt.Errorf("iceberg: table %s still exists; failed to release abandoned purge lock: %w", strings.Join(ident, "."), err)
		}
		if err := removePurgeJournal(journalFS, journalPath); err != nil {
			return fmt.Errorf("iceberg: table %s still exists; failed to discard abandoned purge journal: %w", strings.Join(ident, "."), err)
		}
		return nil
	}
	if !isMissingTableOrNamespace(loadErr) {
		return fmt.Errorf("iceberg: could not confirm table %s is absent before journaled physical purge: %w", strings.Join(ident, "."), loadErr)
	}
	if readErr != nil {
		return fmt.Errorf("iceberg: catalog confirms table %s is absent but its purge journal is corrupt and was retained: %w", strings.Join(ident, "."), readErr)
	}
	if err := d.validatePurgeJournalLocation(journal); err != nil {
		return fmt.Errorf("iceberg: unsafe purge journal for table %s: %w", strings.Join(ident, "."), err)
	}
	claimToken, err := d.claimPurgeResume(ctx, ident, journal.TableUUID)
	if err != nil {
		return fmt.Errorf("iceberg: failed to claim purge recovery for table %s: %w", strings.Join(ident, "."), err)
	}
	claimActive := true
	defer func() {
		if !claimActive {
			return
		}
		if err := d.relinquishPurgeClaim(context.WithoutCancel(ctx), ident, journal.TableUUID, claimToken); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("iceberg: failed to relinquish purge recovery claim for %s: %w", strings.Join(ident, "."), err))
		}
	}()
	if err := d.recoverJournalDeletionFence(ctx, journalFS, journalPath, journal); err != nil {
		return fmt.Errorf("iceberg: failed to recover fenced catalog deletion for table %s: %w", strings.Join(ident, "."), err)
	}
	if err := d.validateOrphanCleanupLocation(ctx, ident, journal.TableLocation); err != nil {
		return fmt.Errorf("iceberg: journaled purge isolation check failed for table %s: %w", strings.Join(ident, "."), err)
	}
	tableFS, err := icebergio.LoadFS(ctx, d.cfg.Properties, journal.TableLocation)
	if err != nil {
		return fmt.Errorf("iceberg: failed to load table filesystem for journaled purge: %w", err)
	}
	if _, ok := tableFS.(icebergio.ListableIO); !ok {
		return fmt.Errorf("iceberg: table filesystem cannot list residue for journaled purge")
	}

	if err := executePurgeJournal(ctx, d, tableFS, journalFS, journalPath, journal, claimToken); err != nil {
		return fmt.Errorf("iceberg: failed to resume physical purge for table %s: %w", strings.Join(ident, "."), err)
	}
	claimActive = false
	return nil
}

func (d *Destination) recoverJournalDeletionFence(
	ctx context.Context,
	journalFS icebergio.IO,
	journalPath string,
	journal *purgeJournal,
) error {
	var live *icebergtable.Table
	for _, deletionIdent := range journal.DeletionIdentifiers {
		tbl, err := d.catalog.LoadTable(ctx, deletionIdent)
		if isMissingTableOrNamespace(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to inspect deletion fence %s: %w", strings.Join(deletionIdent, "."), err)
		}
		if tbl.Metadata().TableUUID().String() != journal.TableUUID {
			return fmt.Errorf("deletion fence %s was reused by another table generation", strings.Join(deletionIdent, "."))
		}
		if live != nil {
			return fmt.Errorf("multiple live deletion fences exist for table %s", strings.Join(journal.Identifier, "."))
		}
		live = tbl
	}
	if live == nil {
		return nil
	}
	if d.cfg.Properties.Get("type", "") == "hadoop" {
		if err := d.catalog.DropTable(ctx, live.Identifier()); err != nil && !isMissingTableOrNamespace(err) {
			return fmt.Errorf("failed to remove verified Hadoop deletion fence %s: %w", strings.Join(live.Identifier(), "."), err)
		}
		return nil
	}

	next := deletionFenceIdentifier(journal.Identifier)
	journal.DeletionIdentifiers = append(journal.DeletionIdentifiers, slices.Clone(next))
	if err := writePurgeJournal(journalFS, journalPath, journal); err != nil {
		return fmt.Errorf("failed to persist next deletion fence before recovery: %w", err)
	}
	claimed, err := d.claimTableForDeletionAt(ctx, live, journal.TableUUID, nil, next)
	if err != nil {
		return err
	}
	if err := d.catalog.DropTable(ctx, claimed.Identifier()); err != nil && !isMissingTableOrNamespace(err) {
		remaining, loadErr := d.catalog.LoadTable(context.WithoutCancel(ctx), claimed.Identifier())
		if isMissingTableOrNamespace(loadErr) {
			return nil
		}
		if loadErr == nil && remaining.Metadata().TableUUID().String() != journal.TableUUID {
			return fmt.Errorf("recovery deletion fence was replaced after claiming it")
		}
		return errors.Join(fmt.Errorf("failed to remove recovered deletion fence: %w", err), loadErr)
	}
	return nil
}

func executePurgeJournal(
	ctx context.Context,
	destination *Destination,
	tableFS icebergio.IO,
	journalFS icebergio.IO,
	journalPath string,
	journal *purgeJournal,
	lockToken string,
) error {
	catalog := destination.catalog
	purgeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), clientSidePurgeTimeout)
	defer cancel()

	var purgeErr error
	for attempt := range clientSidePurgeMaxAttempts {
		live, err := catalog.LoadTable(purgeCtx, journal.Identifier)
		if err == nil {
			if live.Metadata().TableUUID().String() != journal.TableUUID {
				if err := destination.releasePurgeLockOwned(purgeCtx, journal.Identifier, journal.TableUUID, lockToken); err != nil {
					return fmt.Errorf("table identifier was reused; failed to release purge lock: %w", err)
				}
				if removeErr := removePurgeJournal(journalFS, journalPath); removeErr != nil {
					return fmt.Errorf("table identifier was reused; failed to discard stale purge journal: %w", removeErr)
				}
				return nil
			}
			return fmt.Errorf("table still exists; refusing physical purge")
		}
		if !isMissingTableOrNamespace(err) {
			return fmt.Errorf("could not confirm catalog absence: %w", err)
		}

		purgeErr = removeTableFiles(purgeCtx, tableFS, journal.Files)
		for purgeErr == nil {
			listable, ok := tableFS.(icebergio.ListableIO)
			if !ok {
				return fmt.Errorf("table filesystem cannot list residue after purge")
			}
			residue, listErr := filesUnderTableLocation(purgeCtx, listable, journal.TableLocation)
			if listErr != nil {
				purgeErr = listErr
			} else {
				for _, file := range residue {
					if !locationContains(journal.TableLocation, file) {
						return fmt.Errorf("listed residue %q escapes table location %q", file, journal.TableLocation)
					}
				}
				if len(residue) == 0 {
					break
				}
				live, loadErr := catalog.LoadTable(purgeCtx, journal.Identifier)
				if loadErr == nil {
					if live.Metadata().TableUUID().String() != journal.TableUUID {
						if err := destination.releasePurgeLockOwned(purgeCtx, journal.Identifier, journal.TableUUID, lockToken); err != nil {
							return fmt.Errorf("table identifier was reused; failed to release purge lock: %w", err)
						}
						if removeErr := removePurgeJournal(journalFS, journalPath); removeErr != nil {
							return fmt.Errorf("table identifier was reused; failed to discard stale purge journal: %w", removeErr)
						}
						return nil
					}
					return fmt.Errorf("table reappeared while purging residue; refusing further deletion")
				}
				if !isMissingTableOrNamespace(loadErr) {
					return fmt.Errorf("could not reconfirm catalog absence before deleting residue: %w", loadErr)
				}
				purgeErr = removeTableFiles(purgeCtx, tableFS, residue)
			}
		}
		if purgeErr == nil {
			if err := destination.releasePurgeLockOwned(purgeCtx, journal.Identifier, journal.TableUUID, lockToken); err != nil {
				return fmt.Errorf("physical files were removed but purge lock could not be released: %w", err)
			}
			if err := removePurgeJournal(journalFS, journalPath); err != nil {
				return fmt.Errorf("physical files were removed but purge journal could not be removed: %w", err)
			}
			return nil
		}
		if attempt == clientSidePurgeMaxAttempts-1 {
			break
		}
		wait := min(100*time.Millisecond<<attempt, 2*time.Second)
		timer := time.NewTimer(wait)
		select {
		case <-purgeCtx.Done():
			timer.Stop()
			return errors.Join(purgeErr, purgeCtx.Err())
		case <-timer.C:
		}
	}
	return purgeErr
}

func (d *Destination) purgeJournalPath(ident icebergtable.Identifier) (string, error) {
	base, err := d.purgeJournalRoot(ident)
	if err != nil {
		return "", err
	}
	name := identifierHash(d.cfg.CatalogName, ident) + ".json"
	return appendLocationPath(base, name, false), nil
}

func (d *Destination) purgeJournalExists(ctx context.Context, ident icebergtable.Identifier) (bool, error) {
	journalPath, err := d.purgeJournalPath(ident)
	if err != nil {
		return false, err
	}
	journalFS, err := icebergio.LoadFS(ctx, d.cfg.Properties, journalPath)
	if err != nil {
		return false, err
	}
	file, err := journalFS.Open(journalPath)
	if isObjectNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := file.Close(); err != nil {
		return false, err
	}
	return true, nil
}

func identifierHash(catalogName string, ident icebergtable.Identifier) string {
	hashInput := catalogName + "\x00" + strings.Join(ident, "\x00")
	sum := sha256.Sum256([]byte(hashInput))
	return hex.EncodeToString(sum[:])
}

func (d *Destination) purgeJournalRoot(ident icebergtable.Identifier) (string, error) {
	base := d.configuredPurgeWarehouse()
	if base == "" || strings.HasPrefix(strings.ToLower(base), "arn:") {
		return "", fmt.Errorf("iceberg: %w", errPurgeJournalRootUnavailable)
	}
	if localBase, ok := localFilesystemPath(base); ok && !filepath.IsAbs(localBase) {
		return "", fmt.Errorf("iceberg: client-side purge requires an absolute filesystem warehouse for durable cleanup journals")
	}
	return appendLocationPath(base, ".ingestr/purge-journals", false), nil
}

func (d *Destination) configuredPurgeWarehouse() string {
	base := d.cfg.PurgeJournalRoot
	if strings.TrimSpace(base) == "" {
		base = d.cfg.Properties.Get("warehouse", "")
	}
	return strings.TrimSuffix(strings.TrimSpace(base), "/")
}

func writePurgeJournal(tableFS icebergio.IO, journalPath string, journal *purgeJournal) error {
	writer, ok := tableFS.(icebergio.WriteFileIO)
	if !ok {
		return fmt.Errorf("filesystem cannot persist durable purge journals")
	}
	payload, err := json.Marshal(journal)
	if err != nil {
		return fmt.Errorf("failed to encode purge journal: %w", err)
	}
	if localPath, ok := localFilesystemPath(journalPath); ok {
		if !filepath.IsAbs(localPath) {
			return fmt.Errorf("purge journal path must be absolute: %s", journalPath)
		}
		return writeLocalPurgeJournal(localPath, payload)
	}
	if err := writer.WriteFile(journalPath, payload); err != nil {
		return fmt.Errorf("failed to persist purge journal %s: %w", journalPath, err)
	}
	written, err := readBoundedIOFile(tableFS, journalPath, 64<<20)
	if err != nil {
		return fmt.Errorf("failed to verify persisted purge journal %s: %w", journalPath, err)
	}
	if !slices.Equal(written, payload) {
		return fmt.Errorf("persisted purge journal %s failed verification", journalPath)
	}
	return nil
}

func ensureJournalDirectory(journalPath string) error {
	if err := os.MkdirAll(filepath.Dir(journalPath), 0o700); err != nil {
		return fmt.Errorf("failed to create purge journal directory: %w", err)
	}
	return nil
}

func writeLocalPurgeJournal(journalPath string, payload []byte) error {
	if err := ensureJournalDirectory(journalPath); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(journalPath), ".purge-journal-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create purge journal temporary file: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(payload); err != nil {
		_ = temp.Close()
		return fmt.Errorf("failed to write purge journal temporary file: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("failed to sync purge journal temporary file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, journalPath); err != nil {
		return fmt.Errorf("failed to atomically publish purge journal: %w", err)
	}
	dir, err := os.Open(filepath.Dir(journalPath))
	if err != nil {
		return fmt.Errorf("failed to open purge journal directory for sync: %w", err)
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return fmt.Errorf("failed to sync purge journal directory: %w", err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("failed to close purge journal directory: %w", err)
	}
	return nil
}

func readPurgeJournal(tableFS icebergio.IO, journalPath string, ident icebergtable.Identifier) (*purgeJournal, error) {
	payload, err := readBoundedIOFile(tableFS, journalPath, 64<<20)
	if err != nil {
		return nil, err
	}
	var journal purgeJournal
	if err := json.Unmarshal(payload, &journal); err != nil {
		return nil, fmt.Errorf("invalid purge journal JSON: %w", err)
	}
	if err := validatePurgeJournal(&journal, ident); err != nil {
		return nil, err
	}
	return &journal, nil
}

func validatePurgeJournal(journal *purgeJournal, ident icebergtable.Identifier) error {
	if journal == nil || journal.Version != purgeJournalVersion {
		return fmt.Errorf("unsupported purge journal version")
	}
	if !slices.Equal(journal.Identifier, ident) {
		return fmt.Errorf("purge journal identifier mismatch")
	}
	parsedUUID, err := uuid.Parse(journal.TableUUID)
	if err != nil || parsedUUID == uuid.Nil {
		return fmt.Errorf("purge journal has invalid table UUID %q", journal.TableUUID)
	}
	if strings.TrimSpace(journal.TableLocation) == "" {
		return fmt.Errorf("purge journal table location is empty")
	}
	for _, deletionIdent := range journal.DeletionIdentifiers {
		if len(deletionIdent) != len(ident) ||
			!slices.Equal(icebergcatalog.NamespaceFromIdent(deletionIdent), icebergcatalog.NamespaceFromIdent(ident)) ||
			!strings.HasPrefix(deletionIdent[len(deletionIdent)-1], deletionFenceTablePrefix) {
			return fmt.Errorf("purge journal has invalid deletion fence identifier %q", strings.Join(deletionIdent, "."))
		}
	}
	for _, file := range journal.Files {
		if !locationContains(journal.TableLocation, file) {
			return fmt.Errorf("purge journal file %q is outside table location %q", file, journal.TableLocation)
		}
	}
	return nil
}

func (d *Destination) validatePurgeJournalLocation(journal *purgeJournal) error {
	warehouse := d.configuredPurgeWarehouse()
	if warehouse == "" {
		return fmt.Errorf("configured filesystem warehouse is required to validate table location %q", journal.TableLocation)
	}
	if d.cfg.TableLocation != "" {
		expected := strings.TrimSuffix(renderTableLocation(d.cfg.TableLocation, journal.Identifier), "/")
		actual := strings.TrimSuffix(journal.TableLocation, "/")
		if actual != expected && !locationContains(expected, actual) {
			return fmt.Errorf("table location %q does not match configured location %q", journal.TableLocation, expected)
		}
		if warehouse != "" && !locationContains(warehouse, journal.TableLocation) {
			return fmt.Errorf("table location %q must be a strict descendant of configured warehouse %q", journal.TableLocation, warehouse)
		}
		if err := validateCanonicalLocalContainment(warehouse, journal.TableLocation); err != nil {
			return err
		}
		return nil
	}
	if warehouse == "" || !locationContains(warehouse, journal.TableLocation) {
		return fmt.Errorf("table location %q is outside configured warehouse %q", journal.TableLocation, warehouse)
	}
	if err := validateCanonicalLocalContainment(warehouse, journal.TableLocation); err != nil {
		return err
	}
	return nil
}

func validateCanonicalLocalContainment(warehouse, tableLocation string) error {
	warehousePath, warehouseLocal := localFilesystemPath(warehouse)
	tablePath, tableLocal := localFilesystemPath(tableLocation)
	if !warehouseLocal || !tableLocal {
		return nil
	}
	canonicalWarehouse, err := resolveExistingPath(warehousePath)
	if err != nil {
		return fmt.Errorf("failed to resolve warehouse path %q: %w", warehousePath, err)
	}
	canonicalTable, err := resolveExistingPath(tablePath)
	if err != nil {
		return fmt.Errorf("failed to resolve table path %q: %w", tablePath, err)
	}
	rel, err := filepath.Rel(canonicalWarehouse, canonicalTable)
	if err != nil || rel == "." || rel == "" || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("resolved table location %q escapes warehouse %q", canonicalTable, canonicalWarehouse)
	}
	return nil
}

func resolveExistingPath(filePath string) (string, error) {
	abs, err := filepath.Abs(filePath)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(abs)
	var missing []string
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return current, nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

func locationContains(root, child string) bool {
	if rootPath, rootLocal := localFilesystemPath(root); rootLocal {
		if childPath, childLocal := localFilesystemPath(child); childLocal {
			rel, err := filepath.Rel(filepath.Clean(rootPath), filepath.Clean(childPath))
			return err == nil && rel != "." && rel != "" && rel != ".." &&
				!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
		}
	}
	rootURL, rootErr := url.Parse(root)
	childURL, childErr := url.Parse(child)
	if rootErr == nil && childErr == nil && (rootURL.Scheme != "" || childURL.Scheme != "") {
		if !strings.EqualFold(rootURL.Scheme, childURL.Scheme) || !strings.EqualFold(rootURL.Host, childURL.Host) {
			return false
		}
		rootPath := path.Clean("/" + strings.TrimPrefix(rootURL.Path, "/"))
		childPath := path.Clean("/" + strings.TrimPrefix(childURL.Path, "/"))
		return childPath != rootPath && strings.HasPrefix(childPath, strings.TrimSuffix(rootPath, "/")+"/")
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(child))
	return err == nil && rel != "." && rel != "" && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func removePurgeJournal(tableFS icebergio.IO, journalPath string) error {
	if err := tableFS.Remove(journalPath); err != nil && !isObjectNotFound(err) {
		return err
	}
	return nil
}

func isObjectNotFound(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || gcerrors.Code(err) == gcerrors.NotFound
}

func (d *Destination) sweepPurgeJournals(ctx context.Context, namespace icebergtable.Identifier) error {
	if d.catalog == nil {
		return nil
	}
	root, err := d.purgeJournalRoot(namespace)
	if err != nil {
		if errors.Is(err, errPurgeJournalRootUnavailable) {
			return nil
		}
		return err
	}
	journalFS, err := icebergio.LoadFS(ctx, d.cfg.Properties, root)
	if err != nil {
		return fmt.Errorf("iceberg: failed to load purge journal filesystem: %w", err)
	}
	listable, ok := journalFS.(icebergio.ListableIO)
	if !ok {
		return fmt.Errorf("iceberg: purge journal filesystem is not listable")
	}
	var journalPaths []string
	err = listable.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if isObjectNotFound(walkErr) {
				return nil
			}
			return walkErr
		}
		if !entry.IsDir() && strings.HasSuffix(filePath, ".json") {
			journalPaths = append(journalPaths, filePath)
		}
		return nil
	})
	if isObjectNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("iceberg: failed to list purge journals: %w", err)
	}
	slices.Sort(journalPaths)
	for _, journalPath := range journalPaths {
		ident, err := readPurgeJournalIdentifier(journalFS, journalPath)
		if err != nil {
			return fmt.Errorf("iceberg: corrupt purge journal %s was retained: %w", journalPath, err)
		}
		expectedPath, err := d.purgeJournalPath(ident)
		if err != nil {
			return err
		}
		if strings.TrimSuffix(journalPath, "/") != strings.TrimSuffix(expectedPath, "/") {
			return fmt.Errorf(
				"iceberg: purge journal filename/hash mismatch for embedded identifier %s: got %s, expected %s; journal was retained",
				strings.Join(ident, "."), journalPath, expectedPath,
			)
		}
		if len(namespace) > 0 && !slices.Equal(icebergcatalog.NamespaceFromIdent(ident), namespace) {
			continue
		}
		if err := d.resumePurgeJournal(ctx, ident); err != nil {
			return err
		}
	}
	return nil
}

func readPurgeJournalIdentifier(journalFS icebergio.IO, journalPath string) (icebergtable.Identifier, error) {
	payload, err := readBoundedIOFile(journalFS, journalPath, 64<<20)
	if err != nil {
		return nil, err
	}
	var header struct {
		Identifier []string `json:"identifier"`
	}
	if err := json.Unmarshal(payload, &header); err != nil {
		return nil, err
	}
	if len(header.Identifier) == 0 {
		return nil, fmt.Errorf("purge journal identifier is empty")
	}
	return header.Identifier, nil
}

func readBoundedIOFile(tableFS icebergio.IO, filePath string, limit int64) (payload []byte, err error) {
	file, err := tableFS.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	payload, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > limit {
		return nil, fmt.Errorf("purge journal exceeds %d-byte size limit", limit)
	}
	return payload, nil
}
