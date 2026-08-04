package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/xraph/blackstart/pkg/bserr"
)

// ErrNotPublished means the product is not there yet.
//
// Distinct from any other failure, and the distinction matters more here than
// it looks. A cycle NOAA has not posted yet is the ordinary state of the most
// recent hour, and a worker that treated it as an error would log a failure
// every tick for working correctly.
var ErrNotPublished = errors.New("not published yet")

// maxMessageBytes caps a single field's download.
//
// A field-hour of HRRR is a few megabytes and the whole product is 151 MB, so
// this sits between them: high enough for any single message, low enough that
// a range request answered with the entire file — which a misconfigured proxy
// or a server ignoring Range will do — fails rather than filling memory.
const maxMessageBytes = 64 << 20

// Client fetches products over HTTP.
type Client struct {
	http *http.Client
}

// New builds a client over an HTTP client.
//
// The timeout belongs to the caller's http.Client rather than being set here:
// an index is ten kilobytes and a field is megabytes, and one deadline for
// both would be wrong for one of them.
func New(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}

	return &Client{http: client}
}

// Index fetches and parses a product's sidecar.
//
// # Errors
//
// Returns ErrNotPublished when the sidecar is absent, which is what a cycle
// that has not been posted yet looks like.
func (c *Client) Index(ctx context.Context, url string) (Records, error) {
	body, err := c.get(ctx, url, "")
	if err != nil {
		return nil, err
	}

	defer func() { _ = body.Close() }()

	records, err := ParseIndex(body)
	if err != nil {
		return nil, fmt.Errorf("weather: index at %s: %w", url, err)
	}

	return records, nil
}

// Range fetches one message by byte range.
//
// A length of zero means "to the end of the file", which is what Locate
// returns for the last record in a sidecar.
//
// # Errors
//
// Returns an error when the server ignored the range and sent the whole
// product. That is worth catching rather than tolerating: the bytes would
// decode as the first message in the file, which is a different field, and
// nothing downstream would notice.
func (c *Client) Range(
	ctx context.Context, url string, offset, length int64,
) ([]byte, error) {
	if offset < 0 || length < 0 {
		return nil, fmt.Errorf(
			"%w: byte range %d+%d is not a range", bserr.ErrInvalid, offset, length)
	}

	// HTTP ranges are inclusive at both ends, so the last byte is offset plus
	// length minus one. Off by one here produces a message short by a byte,
	// which fails to decode with an error a long way from its cause.
	spec := "bytes=" + strconv.FormatInt(offset, 10) + "-"

	if length > 0 {
		spec += strconv.FormatInt(offset+length-1, 10)
	}

	body, err := c.get(ctx, url, spec)
	if err != nil {
		return nil, err
	}

	defer func() { _ = body.Close() }()

	// Bounded, because a server that ignored the range header would otherwise
	// stream the entire 151 MB product into memory.
	bytes, err := io.ReadAll(io.LimitReader(body, maxMessageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("weather: reading %s: %w", url, err)
	}

	if len(bytes) > maxMessageBytes {
		return nil, fmt.Errorf(
			"%w: %s returned more than %d bytes for a byte range; the server "+
				"appears to have ignored it, and the bytes would decode as the "+
				"wrong field", bserr.ErrInvalid, url, maxMessageBytes)
	}

	if length > 0 && int64(len(bytes)) != length {
		return nil, fmt.Errorf(
			"%w: asked %s for %d bytes at %d and received %d; a short message "+
				"does not decode and a long one is not the field that was asked for",
			bserr.ErrInvalid, url, length, offset, len(bytes))
	}

	return bytes, nil
}

// safeURL rejects anything this should not be asked to fetch.
//
// The URL reaches here from a datasource feed row, and an unattended worker is
// what dereferences it. feed.Feed already refuses a non-http scheme at write
// time; this refuses it again at read time, because the two checks guard
// different things — one stops a bad row being stored, and this stops a bad
// row that is already stored from being followed.
//
// Beyond the scheme it blocks link-local addresses, which is where cloud
// metadata endpoints live. That is the one target worth naming in code: a feed
// row pointing at 169.254.169.254 would have an unattended worker fetch
// instance credentials and store them in the byte plane as though they were
// weather. Loopback and private ranges are deliberately allowed — a local
// mirror is a legitimate deployment and the tests here use one.
//
// Restricting hosts further belongs in deployment. The §8 registry is the
// allowed list, and duplicating it as a hostname check would be a second list
// to keep in step with the first.
func safeURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed URL %q: %w", bserr.ErrInvalid, raw, err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf(
			"%w: URL %q uses the %q scheme; only http and https are fetched, "+
				"because an unattended worker dereferences these and a file:// "+
				"entry would have it reading the local filesystem on the strength "+
				"of a database row", bserr.ErrInvalid, raw, parsed.Scheme)
	}

	if parsed.Host == "" {
		return nil, fmt.Errorf("%w: URL %q has no host", bserr.ErrInvalid, raw)
	}

	if address := net.ParseIP(parsed.Hostname()); address != nil {
		if address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() {
			return nil, fmt.Errorf(
				"%w: URL %q addresses %s, which is link-local; cloud metadata "+
					"endpoints live there, and an unattended worker following one "+
					"would fetch instance credentials and store them as weather",
				bserr.ErrInvalid, raw, address)
		}
	}

	return parsed, nil
}

// get issues a request, translating the status codes that mean something
// specific here.
func (c *Client) get(ctx context.Context, url, rangeSpec string) (io.ReadCloser, error) {
	checked, err := safeURL(url)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, checked.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("weather: building a request for %s: %w", url, err)
	}

	if rangeSpec != "" {
		request.Header.Set("Range", rangeSpec)
	}

	// The URL has been through safeURL: the scheme is http or https and the
	// host is not link-local. gosec's taint analysis cannot see that, and
	// hostname allow-listing is deployment's job rather than this package's —
	// see safeURL.
	//nolint:gosec // G704: the URL is validated by safeURL immediately above
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("weather: fetching %s: %w", url, err)
	}

	switch response.StatusCode {
	case http.StatusOK, http.StatusPartialContent:
		return response.Body, nil

	case http.StatusNotFound, http.StatusForbidden:
		// S3 answers a missing key with 403 when listing is denied, which is
		// how an unpublished cycle presents on the public buckets. Both mean
		// the same thing here: come back later.
		_ = response.Body.Close()

		return nil, fmt.Errorf("%w: %s", ErrNotPublished, url)

	default:
		_ = response.Body.Close()

		return nil, fmt.Errorf(
			"weather: fetching %s: %s", url, response.Status)
	}
}
