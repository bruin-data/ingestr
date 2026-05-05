package naming

import (
	"fmt"
	"strings"
)

type Convention string

const (
	Direct    Convention = "direct"
	SnakeCase Convention = "snake_case"
	Auto      Convention = "auto"
	Default   Convention = Auto
)

func ParseConvention(s string) (Convention, error) {
	switch strings.ToLower(s) {
	case "", "auto", "default":
		return Auto, nil
	case "direct":
		return Direct, nil
	case "snake_case":
		return SnakeCase, nil
	default:
		return "", fmt.Errorf("unknown naming convention: %s (valid: direct, snake_case, auto)", s)
	}
}

type NamingConvention interface {
	Normalize(name string) string
	Name() string
}

func Get(convention Convention) NamingConvention {
	switch convention {
	case SnakeCase:
		return &snakeCaseNaming{}
	default:
		return &directNaming{}
	}
}

type directNaming struct{}

func (d *directNaming) Normalize(name string) string {
	return name
}

func (d *directNaming) Name() string {
	return "direct"
}
