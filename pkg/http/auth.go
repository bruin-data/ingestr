package http

import "resty.dev/v3"

type Authenticator interface {
	Apply(req *resty.Request) error
	Name() string
}

type APIKeyAuth struct {
	Key      string
	Value    string
	InHeader bool
}

func NewAPIKeyAuth(key, value string, inHeader bool) *APIKeyAuth {
	return &APIKeyAuth{Key: key, Value: value, InHeader: inHeader}
}

func (a *APIKeyAuth) Apply(req *resty.Request) error {
	if a.InHeader {
		req.SetHeader(a.Key, a.Value)
	} else {
		req.SetQueryParam(a.Key, a.Value)
	}
	return nil
}

func (a *APIKeyAuth) Name() string { return "api-key" }

type BearerAuth struct {
	Token string
}

func NewBearerAuth(token string) *BearerAuth {
	return &BearerAuth{Token: token}
}

func (a *BearerAuth) Apply(req *resty.Request) error {
	req.SetAuthToken(a.Token)
	return nil
}

func (a *BearerAuth) Name() string { return "bearer" }

type BasicAuth struct {
	Username string
	Password string
}

func NewBasicAuth(username, password string) *BasicAuth {
	return &BasicAuth{Username: username, Password: password}
}

func (a *BasicAuth) Apply(req *resty.Request) error {
	req.SetBasicAuth(a.Username, a.Password)
	return nil
}

func (a *BasicAuth) Name() string { return "basic" }
