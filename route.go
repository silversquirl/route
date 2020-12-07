package route

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

var ErrInvalidSpecifier = errors.New("Invalid route: Invalid format specifier")
var ErrUnmatchedBrace = errors.New("Invalid route: Unmatched brace")
var ErrTooFew = errors.New("Invalid route: Less segments than struct fields")
var ErrTooMany = errors.New("Invalid route: More segments than struct fields")
var ErrType = errors.New("Invalid route: Type cannot be parsed")
var ErrMatch = errors.New("Path does not match route")

type Router struct {
	routes []routeInfo
}

type routeInfo struct {
	h  http.Handler
	re *regexp.Regexp
	ty reflect.Type
	ps []parser
}

func NewRouter() *Router {
	return &Router{}
}

func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Iterate backwards so more recently added routes take preference
	for i := len(router.routes) - 1; i >= 0; i-- {
		if router.routes[i].serve(w, r) {
			return
		}
	}
	http.NotFound(w, r)
}

func (route *routeInfo) serve(w http.ResponseWriter, r *http.Request) (ok bool) {
	match := route.re.FindStringSubmatch(r.URL.Path + "/")
	if match == nil {
		return false
	}

	match = match[1:] // Remove full match
	rval := reflect.New(route.ty).Elem()
	for i, parser := range route.ps {
		v, err := parser(match[i])
		if err != nil {
			return false
		}
		rval.Field(i).Set(reflect.ValueOf(v))
	}

	newCtx := context.WithValue(r.Context(), "route", rval.Interface())
	route.h.ServeHTTP(w, r.WithContext(newCtx))
	return true
}

func (router *Router) Handle(route string, routeStruct interface{}, h http.Handler) {
	re, err := buildRouteRegex(route)
	if err != nil {
		panic(err)
	}

	ty := reflect.TypeOf(routeStruct)

	nfmt := re.NumSubexp()
	narg := ty.NumField()

	if nfmt < narg {
		panic(ErrTooFew)
	} else if nfmt > narg {
		panic(ErrTooMany)
	}

	parsers := make([]parser, nfmt)
	for i := 0; i < narg; i++ {
		parsers[i] = parserForType(ty.Field(i).Type)
	}

	router.routes = append(router.routes, routeInfo{h, re, ty, parsers})
}

func (router *Router) HandleFunc(route string, routeStruct interface{}, h func(http.ResponseWriter, *http.Request)) {
	router.Handle(route, routeStruct, http.HandlerFunc(h))
}

func buildRouteRegex(format string) (*regexp.Regexp, error) {
	reb := strings.Builder{}
	reb.WriteByte('^') // Anchor to start of input

	for {
		// Find next format specifier
		begin := strings.IndexByte(format, '{')
		if begin < 0 {
			break
		}
		end := strings.IndexByte(format, '}')
		if end < begin {
			// end is either -1, meaning the opening brace is unmatched, or less than begin, meaning a closing brace is unmatched
			return nil, ErrUnmatchedBrace
		}

		// Write literal section
		if begin > 0 {
			reb.WriteString(regexp.QuoteMeta(format[:begin]))
		}

		// Parse pattern flags
		spec := strings.Trim(format[begin+1:end], " \t\n")
		var optional, includeSlash bool
		for _, ch := range spec {
			switch ch {
			case '?':
				optional = true
			case '/':
				includeSlash = true
			default:
				return nil, ErrInvalidSpecifier
			}
		}

		// Write pattern section
		reb.WriteByte('(')
		reb.WriteString("[^/]+")
		if includeSlash {
			reb.WriteString("(?:/[^/]+)*?")
		}
		reb.WriteByte(')')
		if optional {
			reb.WriteByte('?')
		}

		format = format[end+1:]
	}

	// Write any trailing literal section
	if format != "" {
		reb.WriteString(regexp.QuoteMeta(format))
	}

	reb.WriteString("/*") // Allow trailing slashes
	reb.WriteByte('$')    // Anchor to end of input
	return regexp.Compile(reb.String())
}

type parser func(string) (interface{}, error)

var parsers = map[reflect.Type]parser{
	reflect.TypeOf(""): func(s string) (interface{}, error) {
		return s, nil
	},
	reflect.TypeOf(true): func(s string) (interface{}, error) {
		return strconv.ParseBool(s)
	},
	reflect.TypeOf(0): func(s string) (interface{}, error) {
		return strconv.Atoi(s)
	},
	reflect.TypeOf(0.0): func(s string) (interface{}, error) {
		return strconv.ParseFloat(s, 64)
	},
}

func parserForType(t reflect.Type) parser {
	p, ok := parsers[t]
	if !ok {
		panic(ErrType)
	}
	return p
}
