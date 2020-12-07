package route

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func testHandler(t *testing.T) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		route := r.Context().Value("route")
		if route == nil {
			t.Error("route value is nil")
			return
		}

		enc := json.NewEncoder(w)
		if err := enc.Encode(reflect.TypeOf(route).Name()); err != nil {
			t.Error("error encoding route type name:", err)
			return
		}

		if err := enc.Encode(route); err != nil {
			t.Error("error encoding route value:", err)
			return
		}
	}
}

func testRequest(t *testing.T, h http.Handler, r *http.Request, expect interface{}) {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	v := reflect.ValueOf(expect)
	ty := v.Type()

	gotName := ""
	gotVal := reflect.New(ty).Elem()

	dec := json.NewDecoder(w.Body)
	dec.Decode(&gotName)
	dec.Decode(gotVal.Addr().Interface())

	if gotName != ty.Name() {
		t.Errorf("Type names do not match: expected '%s', got '%s'", ty.Name(), gotName)
	}
	if !reflect.DeepEqual(gotVal.Interface(), expect) {
		t.Errorf("Values to not match: expected %v, got %v", expect, gotVal)
	}
}

func TestStaticRoute(t *testing.T) {
	r := NewRouter()

	type badRoute struct{}
	type goodRoute struct{}

	h := testHandler(t)
	r.HandleFunc("/", badRoute{}, h)
	r.HandleFunc("/foo", goodRoute{}, h)
	r.HandleFunc("/foo/bar", badRoute{}, h)

	testRequest(t, r, httptest.NewRequest("GET", "/foo/", nil), goodRoute{})
}

func TestPatternRoute(t *testing.T) {
	r := NewRouter()

	type badRoute struct{}
	type goodRoute struct{ Foo string }

	h := testHandler(t)
	r.HandleFunc("/", badRoute{}, h)
	r.HandleFunc("/foo", badRoute{}, h) // This rule should never be matched because it's registered before another rule that matches the same location
	r.HandleFunc("/{}", goodRoute{}, h)
	r.HandleFunc("/foo/{}", goodRoute{}, h)

	testRequest(t, r, httptest.NewRequest("GET", "/", nil), badRoute{})
	testRequest(t, r, httptest.NewRequest("GET", "/foo", nil), goodRoute{"foo"})
	testRequest(t, r, httptest.NewRequest("GET", "/foo/", nil), goodRoute{"foo"})
	testRequest(t, r, httptest.NewRequest("GET", "/foo/bar", nil), goodRoute{"bar"})
}

func TestSlashRoute(t *testing.T) {
	r := NewRouter()

	type badRoute struct{}
	type goodRoute struct{ Path string }

	h := testHandler(t)
	r.HandleFunc("/", badRoute{}, h)
	r.HandleFunc("/{/}", goodRoute{}, h)
	r.HandleFunc("/foo/{/}", goodRoute{}, h)
	r.HandleFunc("/quux/{/?}", goodRoute{}, h)

	testRequest(t, r, httptest.NewRequest("GET", "/", nil), badRoute{})

	testRequest(t, r, httptest.NewRequest("GET", "/foo", nil), goodRoute{"foo"})
	testRequest(t, r, httptest.NewRequest("GET", "/foo/", nil), goodRoute{"foo"})
	testRequest(t, r, httptest.NewRequest("GET", "/bar/foo/baz", nil), goodRoute{"bar/foo/baz"})

	testRequest(t, r, httptest.NewRequest("GET", "/foo/bar", nil), goodRoute{"bar"})
	testRequest(t, r, httptest.NewRequest("GET", "/foo/bar/baz/", nil), goodRoute{"bar/baz"})

	testRequest(t, r, httptest.NewRequest("GET", "/quux", nil), goodRoute{""})
	testRequest(t, r, httptest.NewRequest("GET", "/quux/", nil), goodRoute{""})
	testRequest(t, r, httptest.NewRequest("GET", "/quux/frob", nil), goodRoute{"frob"})
	testRequest(t, r, httptest.NewRequest("GET", "/quux/frob/", nil), goodRoute{"frob"})
}
