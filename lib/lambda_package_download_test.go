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

func lambdaPackageLocationForTest(rawURL string, payload []byte) lambdaPackageLocation {
	hash := sha256.Sum256(payload)
	return lambdaPackageLocation{
		url:        rawURL,
		codeSHA256: base64.StdEncoding.EncodeToString(hash[:]),
	}
}

func interruptLambdaPackageResponse(t *testing.T, writer http.ResponseWriter, payload []byte, cut int) {
	t.Helper()
	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		t.Fatal("test server does not support hijacking")
	}
	connection, stream, err := hijacker.Hijack()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fmt.Fprintf(
		stream,
		"HTTP/1.1 200 OK\r\nContent-Length: %d\r\nContent-Type: application/zip\r\n\r\n",
		len(payload),
	)
	_, _ = stream.Write(payload[:cut])
	_ = stream.Flush()
	_ = connection.Close()
}

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
			interruptLambdaPackageResponse(t, writer, payload, cut)
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

	location := lambdaPackageLocationForTest(
		server.URL+"/function.zip?generation=initial&credential=secret",
		payload,
	)
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

func TestDownloadLambdaPackageHTTPClientHasTimeout(t *testing.T) {
	if lambdaPackageDownloadHTTPClient.Timeout != lambdaPackageDownloadAttemptTimeout {
		t.Fatalf(
			"Lambda package HTTP client timeout = %s, want %s",
			lambdaPackageDownloadHTTPClient.Timeout,
			lambdaPackageDownloadAttemptTimeout,
		)
	}
}

func TestDownloadLambdaPackageRestartsWhenServerIgnoresRange(t *testing.T) {
	payload := bytes.Repeat([]byte("Lambda package contents"), 4096)
	cut := len(payload) / 4
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch requests.Add(1) {
		case 1:
			interruptLambdaPackageResponse(t, writer, payload, cut)
		case 2:
			expectedRange := "bytes=" + strconv.Itoa(cut) + "-"
			if request.Header.Get("Range") != expectedRange {
				t.Errorf("resume Range = %q, want %q", request.Header.Get("Range"), expectedRange)
			}
			writer.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = writer.Write(payload)
		default:
			t.Errorf("unexpected request %d", requests.Load())
		}
	}))
	defer server.Close()

	location := lambdaPackageLocationForTest(server.URL+"/function.zip", payload)
	var refreshes atomic.Int32
	actual, err := downloadLambdaPackage(context.Background(), location, func(context.Context) (lambdaPackageLocation, error) {
		refreshes.Add(1)
		return location, nil
	})
	if err != nil {
		t.Fatalf("download Lambda package: %v", err)
	}
	if !bytes.Equal(actual, payload) {
		t.Fatalf("downloaded payload mismatch: got %d bytes, want %d", len(actual), len(payload))
	}
	if requests.Load() != 2 || refreshes.Load() != 1 {
		t.Fatalf("requests = %d, refreshes = %d; want 2, 1", requests.Load(), refreshes.Load())
	}
}

func TestDownloadLambdaPackageRejectsChangedCodeDuringResume(t *testing.T) {
	payload := bytes.Repeat([]byte("original Lambda package"), 4096)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		interruptLambdaPackageResponse(t, writer, payload, len(payload)/3)
	}))
	defer server.Close()

	location := lambdaPackageLocationForTest(server.URL+"/function.zip", payload)
	changed := lambdaPackageLocationForTest(server.URL+"/function.zip", []byte("changed Lambda package"))
	_, err := downloadLambdaPackage(context.Background(), location, func(context.Context) (lambdaPackageLocation, error) {
		return changed, nil
	})
	if err == nil || !strings.Contains(err.Error(), "location changed during download") {
		t.Fatalf("changed Lambda package error = %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("HTTP requests = %d, want 1", requests.Load())
	}
}

func TestDownloadLambdaPackageRejectsInvalidResumeRange(t *testing.T) {
	payload := bytes.Repeat([]byte("range-checked Lambda package"), 4096)
	cut := len(payload) / 3
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		switch requests.Add(1) {
		case 1:
			interruptLambdaPackageResponse(t, writer, payload, cut)
		case 2:
			writer.Header().Set("Content-Length", strconv.Itoa(len(payload)-cut))
			writer.Header().Set("Content-Range", "invalid")
			writer.WriteHeader(http.StatusPartialContent)
			_, _ = writer.Write(payload[cut:])
		default:
			t.Errorf("unexpected request %d", requests.Load())
		}
	}))
	defer server.Close()

	location := lambdaPackageLocationForTest(server.URL+"/function.zip", payload)
	_, err := downloadLambdaPackage(context.Background(), location, func(context.Context) (lambdaPackageLocation, error) {
		return location, nil
	})
	if err == nil || !strings.Contains(err.Error(), "invalid Lambda package Content-Range") {
		t.Fatalf("invalid resume range error = %v", err)
	}
	if requests.Load() != 2 {
		t.Fatalf("HTTP requests = %d, want 2", requests.Load())
	}
}

func TestDownloadLambdaPackageRetriesAfterAttemptTimeout(t *testing.T) {
	payload := bytes.Repeat([]byte("timeout-resilient Lambda package"), 4096)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch requests.Add(1) {
		case 1:
			writer.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			writer.WriteHeader(http.StatusOK)
			writer.(http.Flusher).Flush()
			<-request.Context().Done()
		case 2:
			if request.Header.Get("Range") != "" {
				t.Errorf("retry unexpectedly had Range %q", request.Header.Get("Range"))
			}
			writer.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = writer.Write(payload)
		default:
			t.Errorf("unexpected request %d", requests.Load())
		}
	}))
	defer server.Close()

	location := lambdaPackageLocationForTest(server.URL+"/function.zip", payload)
	client := &http.Client{Timeout: 100 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var refreshes atomic.Int32
	actual, err := downloadLambdaPackageWithClient(ctx, client, location, func(context.Context) (lambdaPackageLocation, error) {
		refreshes.Add(1)
		return location, nil
	})
	if err != nil {
		t.Fatalf("download Lambda package: %v", err)
	}
	if !bytes.Equal(actual, payload) {
		t.Fatalf("downloaded payload mismatch: got %d bytes, want %d", len(actual), len(payload))
	}
	if requests.Load() != 2 || refreshes.Load() != 1 {
		t.Fatalf("requests = %d, refreshes = %d; want 2, 1", requests.Load(), refreshes.Load())
	}
}
