package lib

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloadLambdaPackageResumesAfterConnectionReset(t *testing.T) {
	payload := make([]byte, 512*1024)
	for i := range payload {
		payload[i] = byte((i*31 + 17) % 251)
	}
	cut := len(payload) / 3
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Accept-Encoding") != "identity" {
			t.Errorf("Accept-Encoding = %q, want identity", request.Header.Get("Accept-Encoding"))
		}
		call := requests.Add(1)
		if call == 1 {
			if request.URL.Query().Get("generation") != "initial" {
				t.Errorf("first request did not use the initial signed location")
			}
			if request.Header.Get("Range") != "" {
				t.Errorf("first request unexpectedly had Range %q", request.Header.Get("Range"))
			}
			hijacker, ok := writer.(http.Hijacker)
			if !ok {
				t.Fatal("test server does not support hijacking")
			}
			connection, stream, err := hijacker.Hijack()
			if err != nil {
				t.Fatal(err)
			}
			_, _ = fmt.Fprintf(stream, "HTTP/1.1 200 OK\r\nContent-Length: %d\r\nContent-Type: application/zip\r\n\r\n", len(payload))
			_, _ = stream.Write(payload[:cut])
			_ = stream.Flush()
			_ = connection.Close()
			return
		}
		if request.URL.Query().Get("generation") != "refreshed" {
			t.Errorf("resume request did not use the refreshed signed location")
		}
		expectedRange := "bytes=" + strconv.Itoa(cut) + "-"
		if request.Header.Get("Range") != expectedRange {
			t.Errorf("resume Range = %q, want %q", request.Header.Get("Range"), expectedRange)
		}
		writer.Header().Set("Content-Length", strconv.Itoa(len(payload)-cut))
		writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", cut, len(payload)-1, len(payload)))
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write(payload[cut:])
	}))
	defer server.Close()

	hash := sha256.Sum256(payload)
	location := lambdaPackageLocation{
		url:        server.URL + "/function.zip?generation=initial&credential=secret",
		codeSHA256: base64.StdEncoding.EncodeToString(hash[:]),
	}
	var refreshes atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	actual, err := downloadLambdaPackage(ctx, location, func(context.Context) (lambdaPackageLocation, error) {
		refreshes.Add(1)
		return lambdaPackageLocation{
			url:        server.URL + "/function.zip?generation=refreshed&credential=secret",
			codeSHA256: location.codeSHA256,
		}, nil
	})
	if err != nil {
		t.Fatalf("download Lambda package: %v", err)
	}
	if !bytes.Equal(actual, payload) {
		t.Fatalf("downloaded payload mismatch: got %d bytes, want %d", len(actual), len(payload))
	}
	if requests.Load() != 2 {
		t.Fatalf("HTTP requests = %d, want 2", requests.Load())
	}
	if refreshes.Load() != 1 {
		t.Fatalf("location refreshes = %d, want 1", refreshes.Load())
	}
}

func TestDownloadLambdaPackageDoesNotExposeSignedURL(t *testing.T) {
	const secret = "TOP-SECRET-CREDENTIAL"
	_, err := downloadLambdaPackage(
		context.Background(),
		lambdaPackageLocation{url: "://invalid?credential=" + secret, codeSHA256: "hash"},
		func(context.Context) (lambdaPackageLocation, error) {
			t.Fatal("invalid initial URL unexpectedly refreshed")
			return lambdaPackageLocation{}, nil
		},
	)
	if err == nil {
		t.Fatal("invalid signed URL unexpectedly succeeded")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("download error exposed signed URL credential: %v", err)
	}
}
