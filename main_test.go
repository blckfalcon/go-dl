package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type RoundTripFunc func(req *http.Request) *http.Response

func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil

}

func NewTestClient(fn RoundTripFunc) *http.Client {
	return &http.Client{
		Transport: RoundTripFunc(fn),
	}
}

func TestGetVersions(t *testing.T) {
	jsonResponse := `[{"version":"go1.20.2","stable":true,"files":[{"filename":"go1.20.2.linux-amd64.tar.gz","os":"linux","arch":"amd64","version":"go1.20.2","sha256":"4eaea32f59cde4dc635fbc42161031d13e1c780b87097f4b4234cfce671f1768","size":100107955,"kind":"archive"}]},{"version":"go1.19.7","stable":true,"files":[{"filename":"go1.19.7.linux-amd64.tar.gz","os":"linux","arch":"amd64","version":"go1.19.7","sha256":"7a75720c9b066ae1750f6bcc7052aba70fa3813f4223199ee2a2315fd3eb533d","size":149010475,"kind":"archive"}]}]`

	client := NewTestClient(func(*http.Request) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(jsonResponse)),
		}
	})

	repo := &GoRepository{client: client}

	got, err := repo.GetVersions(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	file1 := File{
		Filename: "go1.20.2.linux-amd64.tar.gz", Os: "linux",
		Arch: "amd64", Version: "go1.20.2",
		Sha256: "4eaea32f59cde4dc635fbc42161031d13e1c780b87097f4b4234cfce671f1768",
		Size:   100107955, Kind: "archive",
	}

	file2 := File{
		Filename: "go1.19.7.linux-amd64.tar.gz", Os: "linux",
		Arch: "amd64", Version: "go1.19.7",
		Sha256: "7a75720c9b066ae1750f6bcc7052aba70fa3813f4223199ee2a2315fd3eb533d",
		Size:   149010475, Kind: "archive",
	}

	want := []Release{
		{
			Version: "go1.20.2",
			Stable:  true,
			Files:   []File{file1},
		},
		{
			Version: "go1.19.7",
			Stable:  true,
			Files:   []File{file2},
		},
	}

	if !reflect.DeepEqual(want, got) {
		t.Errorf("Unexpected versions returned. Want %v, got %v", want, got)
	}
}

func TestGetVersionsUnavailable(t *testing.T) {
	response := "Could not get download page. Try again in a few minutes."

	client := NewTestClient(func(*http.Request) *http.Response {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader(response)),
		}
	})

	repo := &GoRepository{client: client}

	_, err := repo.GetVersions(context.Background())
	if err == nil {
		t.Errorf("Expected to fail on server failure")
	}
}

func TestReleaseSort(t *testing.T) {
	want := []Release{
		{Version: "go1.20.2"},
		{Version: "go1.20"},
		{Version: "go1.19.7"},
		{Version: "go1.18.10"},
	}

	got := []Release{
		{Version: "go1.19.7"},
		{Version: "go1.20"},
		{Version: "go1.20.2"},
		{Version: "go1.18.10"},
	}
	sort.Sort(ByRelease(got))

	if !reflect.DeepEqual(want, got) {
		t.Errorf("Order should count")
	}
}

func TestFilesFilter(t *testing.T) {
	file1 := File{
		Filename: "go1.20.2.linux-amd64.tar.gz", Os: "linux",
		Arch: "amd64", Version: "go1.20.2",
		Kind: "archive",
	}
	file2 := File{
		Filename: "go1.19.7.linux-amd64.tar.gz", Os: "linux",
		Arch: "arm64", Version: "go1.19.7",
		Kind: "archive",
	}
	file3 := File{
		Filename: "go1.20.5.windows-amd64.msi", Os: "windows",
		Arch: "amd64", Version: "go1.20.5",
		Kind: "installer",
	}

	files := Files{file1, file2, file3}

	got := files.Filter(
		func(f File) bool { return f.Arch == "amd64" },
		func(f File) bool { return f.Os == "linux" },
	)
	want := []File{file1}

	if !reflect.DeepEqual(want, got) {
		t.Errorf("Filtered failed")
	}
}

func TestDownload(t *testing.T) {
	var err error
	fileContent := "The quick brown fox jumps over the lazy dog"

	client := NewTestClient(func(*http.Request) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(fileContent)),
		}
	})

	repo := &GoRepository{client: client}
	file := File{}

	f, err := os.OpenFile(".tmpDownload", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	if err != nil {
		t.Fatal("Was not possible to create a file")
	}

	err = repo.Download(context.Background(), file, f)
	if err != nil {
		t.Fatal("Unexpected download failure")
	}

	dl, err := os.Open(".tmpDownload")
	if errors.Is(err, os.ErrNotExist) {
		t.Fatal("Expected to have the file created")
	}
	defer os.Remove(".tmpDownload")

	got, err := io.ReadAll(dl)
	if err != nil {
		t.Fatalf("Unexpected error reading downloaded file")
	}
	defer dl.Close()

	if fileContent != string(got) {
		t.Errorf("wrong file content")
	}
}
