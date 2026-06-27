package sharepoint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/bruin-data/ingestr/internal/config"
	httpclient "github.com/bruin-data/ingestr/pkg/http"
)

const graphBase = "https://graph.microsoft.com/v1.0"

// graphScopes is the client-credentials scope for the Microsoft Graph API.
// The ".default" scope grants the application permissions configured on the
// Azure AD app registration.
var graphScopes = []string{"https://graph.microsoft.com/.default"}

// userAgent decorates requests so SharePoint throttles them less.
// Microsoft's expected format: ISV|CompanyName|AppName/Version.
const userAgent = "ISV|Bruin|ingestr/1.0"

// graphClient talks to the Microsoft Graph API for a single SharePoint site.
// Authentication uses the OAuth2 client-credentials flow via azidentity, which
// caches and refreshes the access token internally.
type graphClient struct {
	http        *httpclient.Client
	cred        *azidentity.ClientSecretCredential
	hostname    string
	sitePath    string
	library     string // requested document library name; empty => default drive
	siteID      string
	driveID     string // resolved when a non-default library is requested
	maxFileSize int64  // max bytes per downloaded file; 0 => unlimited
	maxFiles    int    // max files a glob may match; 0 => unlimited
}

// driveItem is a single entry returned by the Graph children endpoint. The
// presence of the File / Folder facet distinguishes files from subfolders.
type driveItem struct {
	Name   string          `json:"name"`
	File   json.RawMessage `json:"file"`
	Folder json.RawMessage `json:"folder"`
}

func (d driveItem) isFile() bool   { return len(d.File) > 0 }
func (d driveItem) isFolder() bool { return len(d.Folder) > 0 }

type childrenResponse struct {
	Value    []driveItem `json:"value"`
	NextLink string      `json:"@odata.nextLink"`
}

func newGraphClient(cfg connConfig) (*graphClient, error) {
	cred, err := azidentity.NewClientSecretCredential(cfg.tenantID, cfg.clientID, cfg.clientSecret, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create SharePoint client credential: %w", err)
	}

	client := httpclient.New(
		httpclient.WithTimeout(120*time.Second),
		httpclient.WithRetry(5, 2*time.Second, 30*time.Second),
		httpclient.WithUserAgent(userAgent),
		httpclient.WithDebug(config.DebugMode),
	)

	return &graphClient{
		http:        client,
		cred:        cred,
		hostname:    cfg.hostname,
		sitePath:    cfg.sitePath, // already trimmed in parseURI
		library:     cfg.library,
		maxFileSize: cfg.maxFileSize,
		maxFiles:    cfg.maxFiles,
	}, nil
}

func (g *graphClient) close() error {
	if g.http != nil {
		return g.http.Close()
	}
	return nil
}

// connect resolves the site id from the configured hostname + site path.
func (g *graphClient) connect(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/sites/%s:/%s", graphBase, g.hostname, encodePath(g.sitePath))
	var site struct {
		ID string `json:"id"`
	}
	if err := g.getJSON(ctx, endpoint, &site); err != nil {
		return fmt.Errorf("failed to resolve SharePoint site %q on %q: %w", g.sitePath, g.hostname, err)
	}
	if site.ID == "" {
		return fmt.Errorf("SharePoint site %q on %q resolved to an empty id", g.sitePath, g.hostname)
	}
	g.siteID = site.ID
	config.Debug("[SHAREPOINT] resolved site id: %s", g.siteID)

	// Resolve a non-default document library to its drive id. An empty or
	// "Documents" library uses the site's default drive.
	if g.library != "" && !strings.EqualFold(g.library, "Documents") {
		if err := g.resolveDrive(ctx); err != nil {
			return err
		}
		config.Debug("[SHAREPOINT] resolved library %q to drive %s", g.library, g.driveID)
	}
	return nil
}

type driveInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// resolveDrive finds the drive id for the requested document library by name.
func (g *graphClient) resolveDrive(ctx context.Context) error {
	endpoint := fmt.Sprintf("%s/sites/%s/drives?$select=id,name", graphBase, g.siteID)
	var available []string
	for endpoint != "" {
		if err := ctx.Err(); err != nil {
			return err
		}
		var page struct {
			Value    []driveInfo `json:"value"`
			NextLink string      `json:"@odata.nextLink"`
		}
		if err := g.getJSON(ctx, endpoint, &page); err != nil {
			return fmt.Errorf("failed to list document libraries on site %q: %w", g.sitePath, err)
		}
		for _, d := range page.Value {
			if strings.EqualFold(d.Name, g.library) {
				g.driveID = d.ID
				return nil
			}
			available = append(available, d.Name)
		}
		endpoint = page.NextLink
	}
	return fmt.Errorf("document library %q not found on site %q; available libraries: %v", g.library, g.sitePath, available)
}

// drivePath returns the Graph API base for the active drive (the resolved
// library drive, or the site's default drive).
func (g *graphClient) drivePath() string {
	if g.driveID != "" {
		return fmt.Sprintf("%s/drives/%s", graphBase, g.driveID)
	}
	return fmt.Sprintf("%s/sites/%s/drive", graphBase, g.siteID)
}

func (g *graphClient) token(ctx context.Context) (string, error) {
	tok, err := g.cred.GetToken(ctx, policy.TokenRequestOptions{Scopes: graphScopes})
	if err != nil {
		return "", fmt.Errorf("failed to acquire Microsoft Graph token: %w", err)
	}
	return tok.Token, nil
}

func (g *graphClient) getJSON(ctx context.Context, endpoint string, out interface{}) error {
	tok, err := g.token(ctx)
	if err != nil {
		return err
	}
	resp, err := g.http.R(ctx).SetHeader("Authorization", "Bearer "+tok).Get(endpoint)
	if err != nil {
		return fmt.Errorf("graph request to %s failed: %w", endpoint, err)
	}
	if !resp.IsSuccess() {
		return fmt.Errorf("graph request to %s failed: %s", endpoint, graphErrorMessage(resp.StatusCode(), resp.Body()))
	}
	if err := json.Unmarshal(resp.Body(), out); err != nil {
		return fmt.Errorf("failed to parse graph response from %s: %w", endpoint, err)
	}
	return nil
}

// graphErrorMessage turns a Graph error response into a concise, actionable
// message. Graph returns {"error":{"code","message"}}; when present those are
// surfaced (with hints for the common auth/path mistakes) instead of dumping the
// raw response body.
func graphErrorMessage(status int, body []byte) string {
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &env) == nil && env.Error.Message != "" {
		msg := fmt.Sprintf("status %d (%s): %s", status, env.Error.Code, env.Error.Message)
		switch status {
		case 403:
			msg += " — the app registration may lack the required application permission (Sites.Read.All or Files.Read.All) with admin consent"
		case 404:
			msg += " — check the hostname, site path, library, and file path"
		}
		return msg
	}
	return fmt.Sprintf("status %d: %s", status, truncate(string(body), 512))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// downloadToFile streams a file at a drive-root-relative path to destPath on
// local disk. Streaming (rather than buffering the whole file in memory) keeps
// peak memory low for large workbooks; the response body is copied in chunks.
// On error destPath may be left partially written; the caller owns its removal.
//
// This goes through the raw resty client rather than the httpclient wrapper
// because it needs SetDoNotParseResponse to stream the body; it still inherits
// the client's retry/backoff middleware.
func (g *graphClient) downloadToFile(ctx context.Context, filePath, destPath string) error {
	tok, err := g.token(ctx)
	if err != nil {
		return err
	}
	endpoint := fmt.Sprintf("%s/root:/%s:/content", g.drivePath(), encodePath(filePath))
	config.Debug("[SHAREPOINT] downloading %s", filePath)

	resp, err := g.http.Resty().R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+tok).
		SetDoNotParseResponse(true).
		Get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to download %q: %w", filePath, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !resp.IsSuccess() {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return fmt.Errorf("failed to download %q: %s", filePath, graphErrorMessage(resp.StatusCode(), body))
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create local file for %q: %w", filePath, err)
	}

	// Cap the download so a huge (or malicious) file can't exhaust local disk:
	// read at most maxFileSize+1 bytes and treat overflow as an error.
	var body io.Reader = resp.Body
	if g.maxFileSize > 0 {
		body = io.LimitReader(resp.Body, g.maxFileSize+1)
	}
	n, copyErr := io.Copy(out, body)
	if cerr := out.Close(); copyErr == nil {
		copyErr = cerr
	}
	if copyErr != nil {
		return fmt.Errorf("failed to write %q to disk: %w", filePath, copyErr)
	}
	if g.maxFileSize > 0 && n > g.maxFileSize {
		return fmt.Errorf("file %q exceeds max_file_size of %d bytes", filePath, g.maxFileSize)
	}
	config.Debug("[SHAREPOINT] downloaded %d bytes from %s", n, filePath)
	return nil
}

// listMatching returns drive-root-relative paths of files matching the pattern.
// A pattern with no glob metacharacters is treated as a literal single file and
// returned as-is (the download will surface a clear error if it is missing).
func (g *graphClient) listMatching(ctx context.Context, pattern string) ([]string, error) {
	pattern = strings.TrimPrefix(strings.TrimSpace(pattern), "/")
	if !hasGlobMeta(pattern) {
		return []string{pattern}, nil
	}

	prefix := strings.Trim(extractPrefix(pattern), "/")
	recursive := strings.Contains(pattern, "**")

	var matches []string
	err := g.walk(ctx, prefix, recursive, func(p string) error {
		if ok, _ := doublestar.Match(pattern, p); ok {
			matches = append(matches, p)
			if g.maxFiles > 0 && len(matches) > g.maxFiles {
				return fmt.Errorf("glob %q matched more than max_files=%d files; narrow the pattern or raise max_files", pattern, g.maxFiles)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// walk lists the files under folder, invoking emit for each file. When
// recursive is true it descends into subfolders. emit may return an error to
// stop the walk early (e.g. when a match cap is hit).
func (g *graphClient) walk(ctx context.Context, folder string, recursive bool, emit func(string) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	items, err := g.listChildren(ctx, folder)
	if err != nil {
		return err
	}
	for _, item := range items {
		full := item.Name
		if folder != "" {
			full = folder + "/" + item.Name
		}
		switch {
		case item.isFolder():
			if recursive {
				if err := g.walk(ctx, full, recursive, emit); err != nil {
					return err
				}
			}
		case item.isFile():
			if err := emit(full); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *graphClient) listChildren(ctx context.Context, folder string) ([]driveItem, error) {
	folder = strings.Trim(folder, "/")
	var endpoint string
	if folder == "" {
		endpoint = fmt.Sprintf("%s/root/children", g.drivePath())
	} else {
		endpoint = fmt.Sprintf("%s/root:/%s:/children", g.drivePath(), encodePath(folder))
	}

	var items []driveItem
	for endpoint != "" {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var page childrenResponse
		if err := g.getJSON(ctx, endpoint, &page); err != nil {
			return nil, fmt.Errorf("failed to list folder %q: %w", folder, err)
		}
		items = append(items, page.Value...)
		endpoint = page.NextLink
	}
	return items, nil
}

// encodePath percent-encodes each path segment while preserving the slash
// separators required by the Graph driveItem-by-path syntax.
func encodePath(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func hasGlobMeta(s string) bool {
	return strings.ContainsAny(s, "*?[{")
}

// extractPrefix returns the literal directory prefix of a glob pattern (the
// portion up to and including the last slash before the first metacharacter).
func extractPrefix(pattern string) string {
	idx := strings.IndexAny(pattern, "*?[{")
	if idx == -1 {
		return pattern
	}
	lastSlash := strings.LastIndex(pattern[:idx], "/")
	if lastSlash == -1 {
		return ""
	}
	return pattern[:lastSlash+1]
}
