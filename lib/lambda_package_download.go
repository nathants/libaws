package lib

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/avast/retry-go"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

type lambdaPackageLocation struct {
	url        string
	codeSHA256 string
}

func lambdaPackageLocationFromOutput(out *lambda.GetFunctionOutput) (lambdaPackageLocation, error) {
	if out == nil || out.Code == nil || out.Code.Location == nil || out.Configuration == nil || out.Configuration.CodeSha256 == nil {
		return lambdaPackageLocation{}, fmt.Errorf("lambda function returned no package location or code hash")
	}
	return lambdaPackageLocation{
		url:        aws.ToString(out.Code.Location),
		codeSHA256: aws.ToString(out.Configuration.CodeSha256),
	}, nil
}

const lambdaPackageDownloadMaxBytes = 64 * 1024 * 1024

var lambdaPackageContentRangePattern = regexp.MustCompile(`^bytes ([0-9]+)-([0-9]+)/([0-9]+)$`)

func lambdaPackageContentRange(value string) (int64, int64, int64, error) {
	matches := lambdaPackageContentRangePattern.FindStringSubmatch(value)
	if matches == nil {
		return 0, 0, 0, errors.New("invalid Lambda package Content-Range")
	}
	values := make([]int64, 3)
	for i := range values {
		parsed, err := strconv.ParseInt(matches[i+1], 10, 64)
		if err != nil {
			return 0, 0, 0, errors.New("invalid Lambda package Content-Range integer")
		}
		values[i] = parsed
	}
	return values[0], values[1], values[2], nil
}

func lambdaPackageDownloadError(err error) error {
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return fmt.Errorf("lambda package download transport failed: %w", urlError.Err)
	}
	return fmt.Errorf("lambda package download failed: %w", err)
}

func downloadLambdaPackage(
	ctx context.Context,
	initial lambdaPackageLocation,
	refresh func(context.Context) (lambdaPackageLocation, error),
) ([]byte, error) {
	if initial.url == "" || initial.codeSHA256 == "" || refresh == nil {
		return nil, errors.New("lambda package download requires a location, code hash, and refresh function")
	}
	expectedCodeHash := initial.codeSHA256
	location := initial
	firstAttempt := true
	data := make([]byte, 0)
	totalBytes := int64(-1)

	err := Retry(ctx, func() error {
		if !firstAttempt {
			refreshed, err := refresh(ctx)
			if err != nil {
				return err
			}
			location = refreshed
		}
		firstAttempt = false
		if location.url == "" || location.codeSHA256 != expectedCodeHash {
			return retry.Unrecoverable(errors.New("lambda package location changed during download"))
		}

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, location.url, nil)
		if err != nil {
			return retry.Unrecoverable(errors.New("invalid lambda package download URL"))
		}
		request.Header.Set("Accept-Encoding", "identity")
		if len(data) > 0 {
			request.Header.Set("Range", fmt.Sprintf("bytes=%d-", len(data)))
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return lambdaPackageDownloadError(err)
		}
		return func() error {
			defer func() { _ = response.Body.Close() }()

			if len(data) > 0 && response.StatusCode == http.StatusOK {
				data = data[:0]
				totalBytes = -1
			}
			if len(data) == 0 {
				if response.StatusCode != http.StatusOK {
					return fmt.Errorf("lambda package download returned HTTP %d", response.StatusCode)
				}
				if response.ContentLength <= 0 || response.ContentLength > lambdaPackageDownloadMaxBytes {
					return retry.Unrecoverable(errors.New("lambda package download has invalid size"))
				}
				totalBytes = response.ContentLength
			} else {
				if response.StatusCode != http.StatusPartialContent {
					return fmt.Errorf("lambda package resume returned HTTP %d", response.StatusCode)
				}
				start, end, total, err := lambdaPackageContentRange(response.Header.Get("Content-Range"))
				if err != nil {
					return retry.Unrecoverable(err)
				}
				if start != int64(len(data)) || end < start || total != totalBytes || end >= total ||
					response.ContentLength != end-start+1 {
					return retry.Unrecoverable(errors.New("lambda package resume range mismatch"))
				}
			}

			remainingLimit := int64(lambdaPackageDownloadMaxBytes-len(data)) + 1
			chunk, readErr := io.ReadAll(io.LimitReader(response.Body, remainingLimit))
			data = append(data, chunk...)
			if len(data) > lambdaPackageDownloadMaxBytes || int64(len(data)) > totalBytes {
				return retry.Unrecoverable(errors.New("lambda package download exceeded its declared size"))
			}
			if readErr != nil {
				return readErr
			}
			if int64(len(chunk)) != response.ContentLength || int64(len(data)) < totalBytes {
				return io.ErrUnexpectedEOF
			}

			hash := sha256.Sum256(data)
			if base64.StdEncoding.EncodeToString(hash[:]) != expectedCodeHash {
				data = data[:0]
				totalBytes = -1
				return errors.New("lambda package download code hash mismatch")
			}
			return nil
		}()
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}
