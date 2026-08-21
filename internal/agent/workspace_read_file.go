package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"denova/internal/workspacechange"
)

const workspaceReadFileResultSchema = "workspace_file.read.v2"

// Keep one selected window bounded even when a file contains a single very
// large line.
const workspaceReadFileMaxSelectedBytes = 1024 * 1024

// workspaceReadFileRevisionMaxBytes caps the full-file read used only to compute
// the cross-turn revision anchor. It is intentionally larger than the per-window
// selection cap: the window body is what the model sees, while this anchor lets a
// later turn detect whether the file changed since it was read. Files above this
// cap simply carry no anchor, so retention falls back to the compact receipt.
const workspaceReadFileRevisionMaxBytes = 4 * 1024 * 1024

var workspaceReadFileToolDescription = fmt.Sprintf(`Read a text file. Set revision_only=true to return only compact metadata (path, revision and size) without file content; use this after a revision conflict or when only the current revision is needed. Otherwise returns a bounded, line-numbered window: file_path must be absolute, defaults to line 1 and %d lines, and offset/limit select another window. The first line is JSON metadata; pass its revision and source line numbers to edit_file.`, agentFileReadDefaultLimitLines)

type workspaceReadFileInput struct {
	FilePath     string `json:"file_path" jsonschema:"required,description=Absolute text-file path"`
	Offset       int    `json:"offset,omitempty" jsonschema:"description=1-based first line; defaults to 1; ignored when revision_only is true"`
	Limit        int    `json:"limit,omitempty" jsonschema:"description=Maximum lines; defaults to 300; ignored when revision_only is true"`
	RevisionOnly bool   `json:"revision_only,omitempty" jsonschema:"description=Return only compact metadata with current revision and size; do not return file content"`
}

type workspaceReadFileMetadata struct {
	Schema       string `json:"schema"`
	FilePath     string `json:"file_path"`
	Offset       int    `json:"offset,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	RevisionOnly bool   `json:"revision_only,omitempty"`
	Size         int64  `json:"size,omitempty"`
	// Revision anchors the full-file content identity at read time. A later turn
	// compares it against the file's current revision to decide whether the body
	// already in context is still fresh (keep verbatim) or stale (drop to a
	// receipt and force a targeted re-read). Empty when the file exceeds
	// workspaceReadFileRevisionMaxBytes or its revision cannot be computed.
	Revision string `json:"revision,omitempty"`
	// Refreshed marks a body that cross-turn assembly has rewritten to the
	// file's CURRENT content (same offset/limit window) because the file
	// changed after the original read — typically through the model's own
	// edit_file / write_file calls. A refreshed body's line numbers and
	// revision are current at the start of the turn, so the model can keep
	// editing by line without re-reading. It is never set by the read_file
	// tool itself.
	Refreshed bool `json:"refreshed,omitempty"`
}

// workspaceFileSelectionReader lets the production backend keep reads rooted
// inside the active workspace while selecting only the requested window.
type workspaceFileSelectionReader interface {
	ReadFileSelection(context.Context, *filesystem.ReadRequest) (string, error)
}

func newWorkspaceReadFileTool(backend filesystem.Backend, workspaces ...string) (tool.BaseTool, error) {
	if backend == nil {
		return nil, fmt.Errorf("filesystem backend is nil")
	}
	workspace := ""
	if len(workspaces) > 0 {
		workspace = strings.TrimSpace(workspaces[0])
	}
	return utils.InferTool("read_file", workspaceReadFileToolDescription, func(ctx context.Context, input workspaceReadFileInput) (string, error) {
		filePath, _, err := resolveWorkspaceReadPath(workspace, input.FilePath)
		if err != nil {
			return "", err
		}
		if input.RevisionOnly {
			return marshalWorkspaceRevisionOnlyMetadata(workspace, filePath)
		}
		offset, limit := normalizeWorkspaceReadWindow(input.Offset, input.Limit)
		content, err := readWorkspaceFileSelection(ctx, backend, &filesystem.ReadRequest{
			FilePath: filePath,
			Offset:   offset,
			Limit:    limit,
		})
		if err != nil {
			return "", err
		}
		metadata, err := json.Marshal(workspaceReadFileMetadata{
			Schema:   workspaceReadFileResultSchema,
			FilePath: filePath,
			Offset:   offset,
			Limit:    limit,
			// Anchor against the full file so a later turn can tell whether the
			// window it kept is still current. Best-effort: an unreadable or
			// oversized file just yields an empty anchor.
			Revision: workspaceFileRevision(workspace, filePath),
		})
		if err != nil {
			return "", fmt.Errorf("serialize read_file metadata: %w", err)
		}
		return string(metadata) + "\n" + formatWorkspaceLineNumbers(content, offset), nil
	})
}

func marshalWorkspaceRevisionOnlyMetadata(workspace, filePath string) (string, error) {
	absolute, rel, err := resolveWorkspaceReadPath(workspace, filePath)
	if err != nil {
		return "", err
	}
	content, revision, ok := readWorkspaceFullFile(workspace, absolute, rel)
	if !ok {
		return "", fmt.Errorf("cannot compute revision for file (missing, unreadable, or larger than %d bytes): %s", workspaceReadFileRevisionMaxBytes, filePath)
	}
	metadata, err := json.Marshal(workspaceReadFileMetadata{
		Schema:       workspaceReadFileResultSchema,
		FilePath:     absolute,
		RevisionOnly: true,
		Size:         int64(len(content)),
		Revision:     revision,
	})
	if err != nil {
		return "", fmt.Errorf("serialize read_file revision metadata: %w", err)
	}
	return string(metadata), nil
}

func readWorkspaceFileSelection(ctx context.Context, backend filesystem.Backend, req *filesystem.ReadRequest) (string, error) {
	if reader, ok := backend.(workspaceFileSelectionReader); ok {
		return reader.ReadFileSelection(ctx, req)
	}
	selected, err := backend.Read(ctx, req)
	if err != nil {
		return "", err
	}
	if selected == nil {
		return "", fmt.Errorf("no content found at path: %s", req.FilePath)
	}
	if len(selected.Content) > workspaceReadFileMaxSelectedBytes {
		return "", fmt.Errorf(
			"selected read_file window exceeds %d bytes; use a narrower offset/limit or split the long line",
			workspaceReadFileMaxSelectedBytes,
		)
	}
	return selected.Content, nil
}

func (b *agentFilesystemBackend) ReadFileSelection(ctx context.Context, req *filesystem.ReadRequest) (string, error) {
	if req == nil {
		return "", fmt.Errorf("read request is nil")
	}
	if b == nil || b.Backend == nil {
		return "", fmt.Errorf("filesystem backend is nil")
	}
	filePath, rel, err := resolveWorkspaceReadPath(b.workspace, req.FilePath)
	if err != nil {
		return "", err
	}
	var file *os.File
	if b.workspace != "" {
		root, rootErr := os.OpenRoot(b.workspace)
		if rootErr != nil {
			return "", rootErr
		}
		defer root.Close()
		file, err = root.Open(filepath.FromSlash(rel))
	} else {
		file, err = os.Open(filePath)
	}
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("file not found: %s", filePath)
		}
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()
	return selectWorkspaceFileWindow(ctx, file, req.Offset, req.Limit)
}

func selectWorkspaceFileWindow(ctx context.Context, source io.Reader, offset, limit int) (string, error) {
	offset, limit = normalizeWorkspaceReadWindow(offset, limit)
	reader := bufio.NewReaderSize(&contextFileReader{ctx: ctx, reader: source}, 64*1024)
	var selected strings.Builder
	lineNumber := 1
	selectedLines := 0
	for {
		fragment, err := reader.ReadSlice('\n')
		selecting := lineNumber >= offset && selectedLines < limit
		if selecting && len(fragment) > 0 {
			if selected.Len()+len(fragment) > workspaceReadFileMaxSelectedBytes {
				return "", fmt.Errorf(
					"selected read_file window exceeds %d bytes; use a narrower offset/limit or split the long line",
					workspaceReadFileMaxSelectedBytes,
				)
			}
			selected.Write(fragment)
		}
		lineEnded := len(fragment) > 0 && fragment[len(fragment)-1] == '\n'
		if lineEnded || (errors.Is(err, io.EOF) && len(fragment) > 0) {
			if selecting {
				selectedLines++
			}
			lineNumber++
			if selectedLines >= limit {
				break
			}
		}
		if err != nil {
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			if err != io.EOF {
				return "", fmt.Errorf("error reading file: %w", err)
			}
			break
		}
	}
	return selected.String(), nil
}

func resolveWorkspaceReadPath(workspace, input string) (absolute, relative string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", fmt.Errorf("file_path is required")
	}
	if !filepath.IsAbs(input) {
		return "", "", fmt.Errorf("file_path must be absolute: %s", input)
	}
	absolute = filepath.Clean(input)
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return absolute, "", nil
	}
	workspace, err = filepath.Abs(workspace)
	if err != nil {
		return "", "", err
	}
	relative, err = filepath.Rel(filepath.Clean(workspace), absolute)
	if err != nil {
		return "", "", err
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("file_path is outside the active workspace: %s", absolute)
	}
	return absolute, filepath.ToSlash(relative), nil
}

// workspaceFileRevision returns the full-file content revision anchor used for
// cross-turn retention. Results are memoized per resolved path and validated
// against the file's current (size, mtime), so unchanged files skip both the
// full read and the hash. A stale hit can only happen when an external writer
// rewrites a file to the exact same size within one filesystem timestamp tick;
// every mutation still re-validates against freshly hashed bytes inside
// workspacechange, so the anchor is advisory and self-healing.
func workspaceFileRevision(workspace, filePath string) string {
	absolute, rel, err := resolveWorkspaceReadPath(workspace, filePath)
	if err != nil {
		return ""
	}
	if revision, ok := cachedWorkspaceFileRevision(absolute); ok {
		return revision
	}
	_, revision, ok := readWorkspaceFullFile(workspace, absolute, rel)
	if !ok {
		return ""
	}
	return revision
}

// workspaceRevisionAnchor is a stat-validated (size, mtime) -> revision entry
// for one resolved absolute path.
type workspaceRevisionAnchor struct {
	size     int64
	modTime  time.Time
	revision string
}

var workspaceRevisionAnchors sync.Map // string -> workspaceRevisionAnchor

// cachedWorkspaceFileRevision resolves a revision through stat alone. It must
// only be consulted for advisory anchors, never as a substitute for the fresh
// byte-level validation inside workspacechange.
func cachedWorkspaceFileRevision(absolute string) (string, bool) {
	cached, ok := workspaceRevisionAnchors.Load(absolute)
	if !ok {
		return "", false
	}
	anchor := cached.(workspaceRevisionAnchor)
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() || info.Size() != anchor.size || !info.ModTime().Equal(anchor.modTime) {
		return "", false
	}
	return anchor.revision, true
}

// rememberWorkspaceFileRevision seeds the anchor cache after a mutation whose
// resulting revision is already known (for example a successful edit_file).
// Seeding keeps the next read_file metadata line and the per-turn context
// assembly resolver cheap without re-reading the freshly written bytes. The
// path may be workspace-relative (as in change receipts) and is resolved to
// the same canonical key the read-side anchor uses.
func rememberWorkspaceFileRevision(workspace, path, revision string) {
	revision = strings.TrimSpace(revision)
	path = strings.TrimSpace(path)
	if revision == "" || path == "" {
		return
	}
	absolute := path
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(workspace, absolute)
	}
	absolute, _, err := resolveWorkspaceReadPath(workspace, absolute)
	if err != nil {
		return
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	workspaceRevisionAnchors.Store(absolute, workspaceRevisionAnchor{
		size:     info.Size(),
		modTime:  info.ModTime(),
		revision: revision,
	})
}

// ToolResultFileView carries one file window's current content plus the
// full-file revision, used to refresh stale read_file bodies at assembly time.
type ToolResultFileView struct {
	Content  string
	Revision string
}

// ToolResultWindowResolver selects the current content of one offset/limit
// window plus the full-file revision. ok is false when the file is missing,
// unreadable, oversized, or the window is empty.
type ToolResultWindowResolver func(path string, offset, limit int) (ToolResultFileView, bool)

// NewWorkspaceRevisionResolver returns a stat-backed revision resolver bound to
// a workspace for keeping unchanged read_file bodies verbatim across turns. It
// hashes the exact same rooted full-file bytes as the read-time anchor, so an
// unchanged file resolves to the identical revision. The stat-validated anchor
// cache makes repeated resolution of untouched files nearly free.
func NewWorkspaceRevisionResolver(workspace string) ToolResultRevisionResolver {
	return func(path string) (string, bool) {
		revision := workspaceFileRevision(workspace, path)
		return revision, revision != ""
	}
}

// NewWorkspaceFileResolver bundles the stat-backed revision resolver with the
// window resolver so cross-turn assembly can both keep unchanged read_file
// bodies verbatim and refresh stale ones (typically changed by the model's own
// edits) to the file's current content.
func NewWorkspaceFileResolver(workspace string) ToolResultFileResolver {
	return ToolResultFileResolver{
		Revision: NewWorkspaceRevisionResolver(workspace),
		Window:   NewWorkspaceWindowResolver(workspace),
	}
}

// NewWorkspaceWindowResolver returns the window resolver used to REFRESH a
// stale read_file body to the file's current content at assembly time. It
// reads the full file once (bounded by the revision cap), re-hashes it, seeds
// the anchor cache, and selects the same offset/limit window the original
// read returned, so line numbers stay comparable across the refresh.
func NewWorkspaceWindowResolver(workspace string) ToolResultWindowResolver {
	return func(path string, offset, limit int) (ToolResultFileView, bool) {
		view, ok := workspaceFileWindowView(workspace, path, offset, limit)
		return view, ok
	}
}

// workspaceFileWindowView is the shared implementation behind
// NewWorkspaceWindowResolver: one bounded full read produces both the fresh
// revision and the requested window.
func workspaceFileWindowView(workspace, path string, offset, limit int) (ToolResultFileView, bool) {
	absolute, rel, err := resolveWorkspaceReadPath(workspace, path)
	if err != nil {
		return ToolResultFileView{}, false
	}
	content, revision, ok := readWorkspaceFullFile(workspace, absolute, rel)
	if !ok {
		return ToolResultFileView{}, false
	}
	window, err := selectWorkspaceFileWindow(context.Background(), bytes.NewReader(content), offset, limit)
	if err != nil || window == "" {
		return ToolResultFileView{}, false
	}
	return ToolResultFileView{Content: window, Revision: revision}, true
}

// readWorkspaceFullFile reads the entire file (bounded by
// workspaceReadFileRevisionMaxBytes) via the workspace root when available,
// hashes it, and seeds the anchor cache. The final bool is false when the file
// is missing, unreadable, or larger than the cap. This is the single byte
// source shared by the read-time anchor and the assembly-time revision
// resolver.
func readWorkspaceFullFile(workspace, absolute, rel string) ([]byte, string, bool) {
	var file *os.File
	var err error
	if strings.TrimSpace(workspace) != "" {
		root, rootErr := os.OpenRoot(workspace)
		if rootErr != nil {
			return nil, "", false
		}
		defer root.Close()
		file, err = root.Open(filepath.FromSlash(rel))
	} else {
		file, err = os.Open(absolute)
	}
	if err != nil {
		return nil, "", false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() > workspaceReadFileRevisionMaxBytes {
		return nil, "", false
	}
	content, err := io.ReadAll(io.LimitReader(file, workspaceReadFileRevisionMaxBytes+1))
	if err != nil || len(content) > workspaceReadFileRevisionMaxBytes {
		return nil, "", false
	}
	revision := workspacechange.Revision(content)
	workspaceRevisionAnchors.Store(absolute, workspaceRevisionAnchor{
		size:     info.Size(),
		modTime:  info.ModTime(),
		revision: revision,
	})
	return content, revision, true
}

type contextFileReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextFileReader) Read(buffer []byte) (int, error) {
	if r.ctx != nil {
		select {
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		default:
		}
	}
	return r.reader.Read(buffer)
}

func normalizeWorkspaceReadWindow(offset, limit int) (int, int) {
	if offset <= 0 {
		offset = 1
	}
	if limit <= 0 {
		limit = agentFileReadDefaultLimitLines
	}
	return offset, limit
}

func formatWorkspaceLineNumbers(content string, startLine int) string {
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	if strings.HasSuffix(content, "\n") {
		lines = lines[:len(lines)-1]
	}
	var result strings.Builder
	for index, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if index < len(lines)-1 {
			fmt.Fprintf(&result, "%6d\t%s\n", startLine+index, line)
		} else {
			fmt.Fprintf(&result, "%6d\t%s", startLine+index, line)
		}
	}
	return result.String()
}
