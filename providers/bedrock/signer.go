package bedrock

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	sigV4Algorithm = "AWS4-HMAC-SHA256"
	sigV4Request   = "aws4_request"
	timeFormat     = "20060102T150405Z"
	dateFormat     = "20060102"
)

// sigV4Signer signs HTTP requests with AWS Signature Version 4.
type sigV4Signer struct {
	accessKeyID     string
	secretAccessKey string
	sessionToken    string
	region          string
	service         string
}

func newSigV4Signer(accessKeyID, secretAccessKey, region string) *sigV4Signer {
	return &sigV4Signer{
		accessKeyID:     accessKeyID,
		secretAccessKey: secretAccessKey,
		region:          region,
		service:         "bedrock",
	}
}

// Sign signs an HTTP request with AWS SigV4. The body parameter must contain
// the full request body bytes (used for payload hashing). The time t is the
// signing time.
func (s *sigV4Signer) Sign(req *http.Request, body []byte, t time.Time) error {
	// Format timestamps.
	datetime := t.UTC().Format(timeFormat)
	date := t.UTC().Format(dateFormat)

	// Set required headers before signing.
	req.Header.Set("x-amz-date", datetime)
	req.Header.Set("host", req.Host)
	if s.sessionToken != "" {
		req.Header.Set("x-amz-security-token", s.sessionToken)
	}

	// 1. Create the canonical request.
	payloadHash := hashSHA256(body)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	canonicalHeaders, signedHeaders := buildCanonicalHeaders(req)
	canonicalRequest := buildCanonicalRequest(req, canonicalHeaders, signedHeaders, payloadHash)

	// 2. Create the string to sign.
	scope := fmt.Sprintf("%s/%s/%s/%s", date, s.region, s.service, sigV4Request)
	stringToSign := buildStringToSign(datetime, scope, canonicalRequest)

	// 3. Calculate the signing key.
	signingKey := deriveSigningKey(s.secretAccessKey, date, s.region, s.service)

	// 4. Calculate the signature.
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	// 5. Add the Authorization header.
	authHeader := fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		sigV4Algorithm, s.accessKeyID, scope, signedHeaders, signature)
	req.Header.Set("Authorization", authHeader)

	return nil
}

// buildCanonicalRequest constructs the canonical request string per the
// AWS SigV4 specification.
func buildCanonicalRequest(req *http.Request, canonicalHeaders, signedHeaders, payloadHash string) string {
	return strings.Join([]string{
		req.Method,
		canonicalURI(req.URL),
		canonicalQueryString(req.URL),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
}

// buildStringToSign constructs the string to sign for SigV4.
func buildStringToSign(datetime, scope, canonicalRequest string) string {
	return strings.Join([]string{
		sigV4Algorithm,
		datetime,
		scope,
		hashSHA256([]byte(canonicalRequest)),
	}, "\n")
}

// buildCanonicalHeaders returns the canonical headers string and the sorted
// signed header names. Only headers required for SigV4 are included.
func buildCanonicalHeaders(req *http.Request) (canonicalHeaders, signedHeaders string) {
	// Collect header keys in lowercase.
	type headerEntry struct {
		key   string
		value string
	}

	var headers []headerEntry
	for key := range req.Header {
		lowerKey := strings.ToLower(key)
		// Include content-type, host, and any x-amz-* headers.
		if lowerKey == "content-type" || lowerKey == "host" || strings.HasPrefix(lowerKey, "x-amz-") {
			// Trim and collapse whitespace in values.
			values := req.Header[key]
			trimmed := make([]string, len(values))
			for i, v := range values {
				trimmed[i] = strings.TrimSpace(v)
			}
			headers = append(headers, headerEntry{
				key:   lowerKey,
				value: strings.Join(trimmed, ","),
			})
		}
	}

	// Sort by header name.
	sort.Slice(headers, func(i, j int) bool {
		return headers[i].key < headers[j].key
	})

	canonicalParts := make([]string, 0, len(headers))
	signedParts := make([]string, 0, len(headers))
	for _, h := range headers {
		canonicalParts = append(canonicalParts, h.key+":"+h.value+"\n")
		signedParts = append(signedParts, h.key)
	}

	return strings.Join(canonicalParts, ""), strings.Join(signedParts, ";")
}

// canonicalURI returns the URI-encoded path component. If the path is empty,
// "/" is used.
func canonicalURI(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

// canonicalQueryString returns the sorted, URI-encoded query string.
func canonicalQueryString(u *url.URL) string {
	query := u.Query()
	if len(query) == 0 {
		return ""
	}

	// Collect and sort query parameters.
	var params []string
	for key, values := range query {
		for _, value := range values {
			params = append(params, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	sort.Strings(params)
	return strings.Join(params, "&")
}

// deriveSigningKey computes the SigV4 signing key using an HMAC chain:
// key -> date -> region -> service -> "aws4_request"
func deriveSigningKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(date))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte(sigV4Request))
	return kSigning
}

// hmacSHA256 returns the HMAC-SHA256 of data using the given key.
func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write(data)
	return h.Sum(nil)
}

// hashSHA256 returns the hex-encoded SHA256 hash of the data.
func hashSHA256(data []byte) string {
	h := sha256.New()
	if len(data) > 0 {
		_, _ = io.Copy(h, strings.NewReader(string(data))) //nolint:errcheck // hash write cannot fail
	}
	return hex.EncodeToString(h.Sum(nil))
}
