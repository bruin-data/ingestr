package http

import (
	"encoding/json"
	"net/http"
	"time"

	"resty.dev/v3"
)

type Response struct {
	resty *resty.Response
}

func wrapResponse(r *resty.Response) *Response {
	return &Response{resty: r}
}

func (r *Response) StatusCode() int {
	return r.resty.StatusCode()
}

func (r *Response) Status() string {
	return r.resty.Status()
}

func (r *Response) IsSuccess() bool {
	return r.resty.IsSuccess()
}

func (r *Response) IsError() bool {
	return r.resty.IsError()
}

func (r *Response) Body() []byte {
	return r.resty.Bytes()
}

func (r *Response) String() string {
	return r.resty.String()
}

func (r *Response) Result() interface{} {
	return r.resty.Result()
}

func (r *Response) Error() interface{} {
	return r.resty.Error()
}

func (r *Response) Header() http.Header {
	return r.resty.Header()
}

func (r *Response) Duration() time.Duration {
	return r.resty.Duration()
}

func (r *Response) Attempt() int {
	if r.resty == nil || r.resty.Request == nil {
		return 0
	}
	return r.resty.Request.Attempt
}

func (r *Response) JSON(v interface{}) error {
	return json.Unmarshal(r.Body(), v)
}
