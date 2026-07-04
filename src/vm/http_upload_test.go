package vm

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestObjectFromHTTPMultipartFiles(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("title", "avatar"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	part, err := writer.CreateFormFile("upload", "hello.txt")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write([]byte("hello file")); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest("POST", "/upload", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.ContentLength = int64(body.Len())

	obj := requestObjectFromHTTP(req, body.Bytes(), ObjectValue{})

	if multipartValue, ok := obj["multipart"].Value.(bool); !ok || !multipartValue {
		t.Fatalf("expected multipart=true, got %#v", obj["multipart"])
	}

	form := obj["form"].Value.(ObjectValue)
	if got := form["title"].Value; got != "avatar" {
		t.Fatalf("expected title field, got %#v", got)
	}

	files := obj["files"].Value.(ObjectValue)
	uploadFiles := files["upload"].Value.(*ArrayValue)
	if len(uploadFiles.Elements) != 1 {
		t.Fatalf("expected one uploaded file, got %d", len(uploadFiles.Elements))
	}

	file := uploadFiles.Elements[0].Value.(ObjectValue)
	if got := file["filename"].Value; got != "hello.txt" {
		t.Fatalf("expected filename hello.txt, got %#v", got)
	}
	if got := file["text"].Value; got != "hello file" {
		t.Fatalf("expected text contents, got %#v", got)
	}
	if got := file["size"].AsInt; got != len("hello file") {
		t.Fatalf("expected size %d, got %d", len("hello file"), got)
	}
	if got := string(file["bytes"].Value.(*BufferValue).Bytes); got != "hello file" {
		t.Fatalf("expected byte contents, got %#v", got)
	}
}

func TestHttpClientMultipartFormDataWithTextFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
			t.Fatalf("expected multipart content type, got %q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if got := r.FormValue("title"); got != "avatar" {
			t.Fatalf("expected title avatar, got %q", got)
		}

		file, header, err := r.FormFile("upload")
		if err != nil {
			t.Fatalf("expected upload file: %v", err)
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if header.Filename != "hello.txt" {
			t.Fatalf("expected filename hello.txt, got %q", header.Filename)
		}
		if string(data) != "hello file" {
			t.Fatalf("expected file contents, got %q", string(data))
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	vm := NewVM(VMInfo{JITDisabled: true})
	body := NewNative(ObjectValue{
		"form": NewNative(ObjectValue{
			"title": NewNative("avatar"),
		}),
		"files": NewNative(&ArrayValue{Elements: []TinyValue{
			NewNative(ObjectValue{
				"field":    NewNative("upload"),
				"filename": NewNative("hello.txt"),
				"text":     NewNative("hello file"),
			}),
		}}),
	})

	result := doHTTPRequest(vm, "http.post", http.MethodPost, server.URL, body, ObjectValue{})
	obj := result.Value.(ObjectValue)
	if got := obj["status"].AsInt; got != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, got)
	}
	if got := obj["body"].Value; got != "ok" {
		t.Fatalf("expected body ok, got %#v", got)
	}
}

func TestHttpClientMultipartFormDataWithFileMapAndBuffer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		file, header, err := r.FormFile("data")
		if err != nil {
			t.Fatalf("expected data file: %v", err)
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if header.Filename != "data.bin" {
			t.Fatalf("expected filename data.bin, got %q", header.Filename)
		}
		if header.Header.Get("Content-Type") != "application/octet-stream" {
			t.Fatalf("expected content type application/octet-stream, got %q", header.Header.Get("Content-Type"))
		}
		if string(data) != "abc" {
			t.Fatalf("expected abc, got %q", string(data))
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	vm := NewVM(VMInfo{JITDisabled: true})
	body := NewNative(ObjectValue{
		"files": NewNative(ObjectValue{
			"data": NewNative(ObjectValue{
				"filename":    NewNative("data.bin"),
				"contentType": NewNative("application/octet-stream"),
				"bytes":       NewNative(&BufferValue{Bytes: []byte("abc")}),
			}),
		}),
	})

	result := doHTTPRequest(vm, "http.post", http.MethodPost, server.URL, body, ObjectValue{})
	obj := result.Value.(ObjectValue)
	if got := obj["status"].AsInt; got != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, got)
	}
}
