package vm

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPResponseHelpersWriteStatusHeadersAndHTML(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	writeServerResponse(recorder, request, NativeHttpResponseValue{
		Type:   HttpHtml,
		Status: http.StatusCreated,
		Value:  NewNative("<strong>ok</strong>"),
		Headers: ObjectValue{
			"X-Test": NewNative("yes"),
		},
	})

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, recorder.Code)
	}
	if recorder.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content type: %q", recorder.Header().Get("Content-Type"))
	}
	if recorder.Header().Get("X-Test") != "yes" {
		t.Fatalf("expected custom header, got %q", recorder.Header().Get("X-Test"))
	}
	if recorder.Body.String() != "<strong>ok</strong>" {
		t.Fatalf("unexpected body: %q", recorder.Body.String())
	}
}

func TestHTTPMethodRoutesSupportDynamicParams(t *testing.T) {
	server := &NativeServerValue{}
	ensureServerRoutes(server)

	server.Routes[http.MethodPatch]["/users/:id"] = NewNative("patched")

	handler, params, ok := findRoute(server.Routes[http.MethodPatch], NewNull(), "/users/42")
	if !ok {
		t.Fatalf("expected dynamic PATCH route to match")
	}
	if handler.Value != "patched" {
		t.Fatalf("unexpected handler: %#v", handler)
	}
	if params["id"].Value != "42" {
		t.Fatalf("expected route param id=42, got %#v", params)
	}
}
