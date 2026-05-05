package http

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

type Paginator interface {
	HasNext() bool
	NextPage(ctx context.Context, client *Client, result any) (*Response, error)
	Reset()
}

type PageNumberPaginator struct {
	Endpoint    string
	PageParam   string
	LimitParam  string
	Limit       int
	CurrentPage int
	hasMore     bool
}

func NewPageNumberPaginator(endpoint string, limit int) *PageNumberPaginator {
	return &PageNumberPaginator{
		Endpoint:    endpoint,
		PageParam:   "page",
		LimitParam:  "limit",
		Limit:       limit,
		CurrentPage: 0,
		hasMore:     true,
	}
}

func (p *PageNumberPaginator) WithPageParam(param string) *PageNumberPaginator {
	p.PageParam = param
	return p
}

func (p *PageNumberPaginator) WithLimitParam(param string) *PageNumberPaginator {
	p.LimitParam = param
	return p
}

func (p *PageNumberPaginator) HasNext() bool {
	return p.hasMore
}

func (p *PageNumberPaginator) NextPage(ctx context.Context, client *Client, result any) (*Response, error) {
	p.CurrentPage++

	resp, err := client.R(ctx).
		SetQueryParam(p.PageParam, strconv.Itoa(p.CurrentPage)).
		SetQueryParam(p.LimitParam, strconv.Itoa(p.Limit)).
		SetResult(result).
		Get(p.Endpoint)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (p *PageNumberPaginator) SetHasMore(hasMore bool) {
	p.hasMore = hasMore
}

func (p *PageNumberPaginator) Reset() {
	p.CurrentPage = 0
	p.hasMore = true
}

type OffsetPaginator struct {
	Endpoint      string
	OffsetParam   string
	LimitParam    string
	Limit         int
	CurrentOffset int
	hasMore       bool
}

func NewOffsetPaginator(endpoint string, limit int) *OffsetPaginator {
	return &OffsetPaginator{
		Endpoint:    endpoint,
		OffsetParam: "offset",
		LimitParam:  "limit",
		Limit:       limit,
		hasMore:     true,
	}
}

func (p *OffsetPaginator) WithOffsetParam(param string) *OffsetPaginator {
	p.OffsetParam = param
	return p
}

func (p *OffsetPaginator) WithLimitParam(param string) *OffsetPaginator {
	p.LimitParam = param
	return p
}

func (p *OffsetPaginator) HasNext() bool {
	return p.hasMore
}

func (p *OffsetPaginator) NextPage(ctx context.Context, client *Client, result any) (*Response, error) {
	resp, err := client.R(ctx).
		SetQueryParam(p.OffsetParam, strconv.Itoa(p.CurrentOffset)).
		SetQueryParam(p.LimitParam, strconv.Itoa(p.Limit)).
		SetResult(result).
		Get(p.Endpoint)
	if err != nil {
		return nil, err
	}

	p.CurrentOffset += p.Limit

	return resp, nil
}

func (p *OffsetPaginator) SetHasMore(hasMore bool) {
	p.hasMore = hasMore
}

func (p *OffsetPaginator) Reset() {
	p.CurrentOffset = 0
	p.hasMore = true
}

type CursorPaginator struct {
	Endpoint    string
	CursorParam string
	LimitParam  string
	Limit       int
	NextCursor  string
	hasMore     bool
	started     bool
}

func NewCursorPaginator(endpoint string, limit int) *CursorPaginator {
	return &CursorPaginator{
		Endpoint:    endpoint,
		CursorParam: "cursor",
		LimitParam:  "limit",
		Limit:       limit,
		hasMore:     true,
	}
}

func (p *CursorPaginator) WithCursorParam(param string) *CursorPaginator {
	p.CursorParam = param
	return p
}

func (p *CursorPaginator) WithLimitParam(param string) *CursorPaginator {
	p.LimitParam = param
	return p
}

func (p *CursorPaginator) HasNext() bool {
	return p.hasMore
}

func (p *CursorPaginator) NextPage(ctx context.Context, client *Client, result any) (*Response, error) {
	req := client.R(ctx).
		SetQueryParam(p.LimitParam, strconv.Itoa(p.Limit)).
		SetResult(result)

	if p.NextCursor != "" {
		req.SetQueryParam(p.CursorParam, p.NextCursor)
	}

	resp, err := req.Get(p.Endpoint)
	if err != nil {
		return nil, err
	}

	p.started = true

	return resp, nil
}

func (p *CursorPaginator) SetNextCursor(cursor string) {
	p.NextCursor = cursor
	p.hasMore = cursor != ""
}

func (p *CursorPaginator) Reset() {
	p.NextCursor = ""
	p.hasMore = true
	p.started = false
}

type LinkHeaderPaginator struct {
	InitialURL string
	NextURL    string
	hasMore    bool
	started    bool
}

func NewLinkHeaderPaginator(initialURL string) *LinkHeaderPaginator {
	return &LinkHeaderPaginator{
		InitialURL: initialURL,
		hasMore:    true,
	}
}

func (p *LinkHeaderPaginator) HasNext() bool {
	return p.hasMore
}

func (p *LinkHeaderPaginator) NextPage(ctx context.Context, client *Client, result any) (*Response, error) {
	url := p.InitialURL
	if p.NextURL != "" {
		url = p.NextURL
	}

	resp, err := client.R(ctx).SetResult(result).Get(url)
	if err != nil {
		return nil, err
	}

	p.NextURL = parseLinkHeader(resp.Header().Get("Link"), "next")
	p.hasMore = p.NextURL != ""
	p.started = true

	return resp, nil
}

func (p *LinkHeaderPaginator) Reset() {
	p.NextURL = ""
	p.hasMore = true
	p.started = false
}

var linkHeaderPattern = regexp.MustCompile(`<([^>]+)>;\s*rel="([^"]+)"`)

func parseLinkHeader(header, rel string) string {
	if header == "" {
		return ""
	}

	for _, part := range strings.Split(header, ",") {
		matches := linkHeaderPattern.FindStringSubmatch(strings.TrimSpace(part))
		if len(matches) == 3 && matches[2] == rel {
			return matches[1]
		}
	}

	return ""
}
