package odoo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	httpclient "github.com/bruin-data/ingestr/pkg/http"
)

const (
	apiAuto    = "auto"
	apiJSON2   = "json2"
	apiJSONRPC = "jsonrpc"

	// The JSON-2 API (/json/2) ships with Odoo 19; /jsonrpc is deprecated there and removed in Odoo 22.
	firstJSON2Version = 19
)

type rpcClient struct {
	http     *httpclient.Client
	api      string
	database string
	uid      int64
	apiKey   string
}

type json2Error struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

type jsonRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
		Data    struct {
			Name    string `json:"name"`
			Message string `json:"message"`
		} `json:"data"`
	} `json:"error"`
}

func (c *rpcClient) call(ctx context.Context, model, method string, kwargs map[string]interface{}) (json.RawMessage, error) {
	if c.api == apiJSON2 {
		return c.callJSON2(ctx, model, method, kwargs)
	}
	return c.callJSONRPC(ctx, "object", "execute_kw", []interface{}{c.database, c.uid, c.apiKey, model, method, []interface{}{}, kwargs})
}

func (c *rpcClient) callJSON2(ctx context.Context, model, method string, kwargs map[string]interface{}) (json.RawMessage, error) {
	path := "/json/2/" + url.PathEscape(model) + "/" + url.PathEscape(method)
	resp, err := c.http.R(ctx).
		SetHeader("Content-Type", "application/json").
		SetHeader("Authorization", "Bearer "+c.apiKey).
		SetBody(kwargs).
		Post(path)
	if err != nil {
		return nil, fmt.Errorf("odoo: %s.%s request failed: %w", model, method, err)
	}
	if !resp.IsSuccess() {
		var e json2Error
		if json.Unmarshal(resp.Body(), &e) == nil && e.Message != "" {
			return nil, fmt.Errorf("odoo: %s.%s returned HTTP %d: %s: %s", model, method, resp.StatusCode(), e.Name, e.Message)
		}
		return nil, fmt.Errorf("odoo: %s.%s returned HTTP %d: %s", model, method, resp.StatusCode(), truncate(resp.String(), 500))
	}
	return resp.Body(), nil
}

func (c *rpcClient) callJSONRPC(ctx context.Context, service, method string, args []interface{}) (json.RawMessage, error) {
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "call",
		"params":  map[string]interface{}{"service": service, "method": method, "args": args},
		"id":      1,
	}
	label := describeRPC(service, method, args)
	resp, err := c.http.R(ctx).SetHeader("Content-Type", "application/json").SetBody(body).Post("/jsonrpc")
	if err != nil {
		return nil, fmt.Errorf("odoo: %s request failed: %w", label, err)
	}
	if !resp.IsSuccess() {
		return nil, fmt.Errorf("odoo: %s returned HTTP %d: %s", label, resp.StatusCode(), truncate(resp.String(), 500))
	}
	var out jsonRPCResponse
	if err := json.Unmarshal(resp.Body(), &out); err != nil {
		return nil, fmt.Errorf("odoo: failed to parse %s response: %w", label, err)
	}
	if out.Error != nil {
		msg := out.Error.Data.Message
		if msg == "" {
			msg = out.Error.Message
		}
		return nil, fmt.Errorf("odoo: %s failed: %s: %s", label, out.Error.Data.Name, msg)
	}
	return out.Result, nil
}

func describeRPC(service, method string, args []interface{}) string {
	if service == "object" && len(args) >= 5 {
		return fmt.Sprintf("%v.%v", args[3], args[4])
	}
	return service + "." + method
}

func (c *rpcClient) authenticate(ctx context.Context, username string) error {
	raw, err := c.callJSONRPC(ctx, "common", "authenticate", []interface{}{c.database, username, c.apiKey, map[string]interface{}{}})
	if err != nil {
		return err
	}
	var uid int64
	if err := json.Unmarshal(raw, &uid); err != nil || uid == 0 {
		return fmt.Errorf("odoo: authentication failed for user %q on database %q: check username and api_key", username, c.database)
	}
	c.uid = uid
	return nil
}

// serverMajorVersion reads /web/version, which only exists from Odoo 19 on; any failure means an older server.
func (c *rpcClient) serverMajorVersion(ctx context.Context) (int, bool) {
	resp, err := c.http.R(ctx).Get("/web/version")
	if err != nil || !resp.IsSuccess() {
		return 0, false
	}
	var v struct {
		VersionInfo []interface{} `json:"version_info"`
	}
	if err := json.Unmarshal(resp.Body(), &v); err != nil || len(v.VersionInfo) == 0 {
		return 0, false
	}
	return parseMajorVersion(v.VersionInfo[0])
}

func parseMajorVersion(v interface{}) (int, bool) {
	switch t := v.(type) {
	case float64:
		return int(t), true
	case string:
		n, err := strconv.Atoi(strings.TrimPrefix(t, "saas~"))
		return n, err == nil
	}
	return 0, false
}

func decodeJSON(raw json.RawMessage, v interface{}) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	return dec.Decode(v)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
