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

// In the event of an invalid route string, Router.Handle may panic with one of the following errors.
var (
	ErrInvalidSpecifier = errors.New("Invalid route: Invalid format specifier")
	ErrUnmatchedBrace   = errors.New("Invalid route: Unmatched brace")
	ErrTooFew           = errors.New("Invalid route: Less segments than struct fields")
	ErrTooMany          = errors.New("Invalid route: More segments than struct fields")
	ErrType             = errors.New("Invalid route: Type cannot be parsed")
)

// PathRoute is a simple route struct that stores only a path. See Router.ServeHTTP for more information.
type PathRoute struct{ Path string }

// Router dispatches HTTP requests to a set of routes.
type Router struct {
	routes []routeInfo
}

type routeInfo struct {
	h  http.Handler
	re *regexp.Regexp
	ty reflect.Type
	ps []parser
}

// NewRouter creates a new Router with no parent.
func NewRouter() *Router {
	return &Router{}
}

// ServeHTTP handles a request, dispatching to the correct route.
// If there is a PathRoute attached to the request, the Path within that will be used instead of r.URL.Path
func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	// If there is a PathRoute stored on the request, use that instead
	if route, ok := r.Context().Value("route").(PathRoute); ok {
		path = "/" + route.Path
	}
	// Ensure the path ends with a slash
	path += "/"

	// Iterate backwards so more recently added routes take preference
	for i := len(router.routes) - 1; i >= 0; i-- {
		if router.routes[i].serve(w, r, path) {
			return
		}
	}
	http.NotFound(w, r)
}

// Attempt to match and serve the route.
func (route *routeInfo) serve(w http.ResponseWriter, r *http.Request, path string) (ok bool) {
	match := route.re.FindStringSubmatch(path)
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

// Handle adds a new route to the router.
// The routeStruct's type should have as many fields as there are placeholders in the route.
// A nil routeStruct is equivalent to struct{}{}.
//
// A value of the same type as routeStruct, filled out with the information stored in the accessed
// path, can be retrieved within the handler using the request's Context, with the key "route".
func (router *Router) Handle(route string, routeStruct interface{}, h http.Handler) {
	re, err := buildRouteRegex(route)
	if err != nil {
		panic(err)
	}

	ty := reflect.TypeOf(routeStruct)
	if ty == nil {
		ty = reflect.TypeOf(struct{}{})
	}

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

// HandleFunc adds a new route to the router using a handler function. See Handle for more information.
func (router *Router) HandleFunc(route string, routeStruct interface{}, h func(http.ResponseWriter, *http.Request)) {
	router.Handle(route, routeStruct, http.HandlerFunc(h))
}

// Child creates a new router that handles all routes with the specified prefix within the parent.
func (router *Router) Child(prefix string) (child *Router) {
	child = NewRouter()
	route := strings.TrimRight(prefix, "/") + "/{/?}"
	router.Handle(route, PathRoute{}, child)
	return child
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
	// string
	reflect.TypeOf(""): func(s string) (interface{}, error) {
		return s, nil
	},

	// bool
	reflect.TypeOf(true): func(s string) (interface{}, error) {
		return strconv.ParseBool(s)
	},

	// complex and float
	reflect.TypeOf(0i): func(s string) (interface{}, error) {
		return strconv.ParseComplex(s, 128)
	},
	reflect.TypeOf(complex64(0i)): func(s string) (interface{}, error) {
		return strconv.ParseComplex(s, 64)
	},
	reflect.TypeOf(0.0): func(s string) (interface{}, error) {
		return strconv.ParseFloat(s, 64)
	},
	reflect.TypeOf(float32(0.0)): func(s string) (interface{}, error) {
		return strconv.ParseFloat(s, 32)
	},

	// int
	reflect.TypeOf(0): func(s string) (interface{}, error) {
		i, err := strconv.ParseInt(s, 0, 0)
		return int(i), err
	},
	reflect.TypeOf(int8(0)): func(s string) (interface{}, error) {
		i, err := strconv.ParseInt(s, 0, 8)
		return int8(i), err
	},
	reflect.TypeOf(int16(0)): func(s string) (interface{}, error) {
		i, err := strconv.ParseInt(s, 0, 16)
		return int16(i), err
	},
	reflect.TypeOf(int32(0)): func(s string) (interface{}, error) {
		i, err := strconv.ParseInt(s, 0, 32)
		return int32(i), err
	},
	reflect.TypeOf(int64(0)): func(s string) (interface{}, error) {
		i, err := strconv.ParseInt(s, 0, 64)
		return int64(i), err
	},

	// uint
	reflect.TypeOf(uint(0)): func(s string) (interface{}, error) {
		i, err := strconv.ParseUint(s, 0, 0)
		return uint(i), err
	},
	reflect.TypeOf(uint8(0)): func(s string) (interface{}, error) {
		i, err := strconv.ParseInt(s, 0, 8)
		return uint8(i), err
	},
	reflect.TypeOf(uint16(0)): func(s string) (interface{}, error) {
		i, err := strconv.ParseInt(s, 0, 16)
		return uint16(i), err
	},
	reflect.TypeOf(uint32(0)): func(s string) (interface{}, error) {
		i, err := strconv.ParseInt(s, 0, 32)
		return uint32(i), err
	},
	reflect.TypeOf(uint64(0)): func(s string) (interface{}, error) {
		i, err := strconv.ParseInt(s, 0, 64)
		return uint64(i), err
	},
}

func parserForType(t reflect.Type) parser {
	p, ok := parsers[t]
	if !ok {
		panic(ErrType)
	}
	return p
}
